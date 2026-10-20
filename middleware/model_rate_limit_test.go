package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	recordKey := "rateLimit:model-utc-record"
	recordRedisRequest(ctx, redisClient, recordKey, 2)
	recorded, err := redisClient.LIndex(ctx, recordKey, 0).Result()
	require.NoError(t, err)
	recordedAt, err := time.Parse(modelRateLimitTimeFormat, recorded)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), recordedAt, 2*time.Second)

	checkKey := "rateLimit:model-utc-check"
	withinWindow := time.Now().UTC().Add(-30 * time.Second).Format(modelRateLimitTimeFormat)
	_, err = redisServer.Push(checkKey, withinWindow, withinWindow)
	require.NoError(t, err)
	admission, allowed, err := reserveRedisSuccessSlot(ctx, redisClient, checkKey, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "an existing UTC timestamp inside the window must remain limited on a non-UTC host")
	assert.Empty(t, admission, "满员拒绝路径不得预置席位")
	count, err := redisClient.LLen(ctx, checkKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "拒绝路径不得改动窗口条目")
}

var modelRateLimitTestUsers atomic.Int64

func TestModelRateLimitStreamFailuresDoNotConsumeSuccessLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, backend := range []string{"memory", "redis"} {
		for _, totalLimit := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/total=%d", backend, totalLimit), func(t *testing.T) {
				userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
				handler := memoryRateLimitHandler(60, totalLimit, 1)
				if backend == "redis" {
					useRateLimitMiniRedis(t)
					handler = redisRateLimitHandler(60, totalLimit, 1)
				}
				router := gin.New()
				router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, handler, func(c *gin.Context) {
					status := relaycommon.NewStreamStatus()
					if c.Param("outcome") == "failed" {
						status.MarkFailed("server_error", "", 0)
					} else {
						status.MarkCompleted()
					}
					common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
					c.Status(http.StatusOK)
				})
				assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				if totalLimit > 0 {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				} else {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
				}
				assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
			})
		}
	}
}

func TestModelMemoryRateLimitReservesConcurrentSuccessAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := gin.New()
	router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, memoryRateLimitHandler(60, 0, 1), func(c *gin.Context) {
		if c.Param("outcome") == "failed" {
			close(entered)
			<-release
			status := relaycommon.NewStreamStatus()
			status.MarkFailed("server_error", "", 0)
			common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		}
		c.Status(http.StatusOK)
	})
	go func() {
		defer close(finished)
		assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
	}()
	<-entered
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	close(release)
	<-finished
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
}

// The memory limiter answers like the Redis limiter: an OpenAI error body
// naming the limit that was reached, not an empty 429.
func TestModelMemoryRateLimitRejectionsReturnOpenAIError(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name                     string
		totalLimit, successLimit int
		messageKey               string
	}{
		{name: "total", totalLimit: 1, messageKey: i18n.MsgRateLimitTotalReached},
		{name: "success", successLimit: 1, messageKey: i18n.MsgRateLimitReached},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
			router := gin.New()
			router.GET("/", func(c *gin.Context) {
				c.Set("id", userID)
				c.Set(common.RequestIdKey, "req-limited")
			}, memoryRateLimitHandler(60, tc.totalLimit, tc.successLimit), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			require.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)

			resp := performRateLimitRequest(router, "/", "127.0.0.1:1000")
			assert.Equal(t, http.StatusTooManyRequests, resp.Code)
			message := i18n.Translate("", tc.messageKey, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": 1})
			want, err := common.Marshal(gin.H{"error": gin.H{
				"message": common.MessageWithRequestId(message, "req-limited"),
				"type":    "new_api_error",
				"code":    "",
			}})
			require.NoError(t, err)
			assert.JSONEq(t, string(want), resp.Body.String())
		})
	}
}

// Infra failure must fail open like the main limiter: a dead Redis may not
// turn model relay traffic into 500s. Enforcement resumes automatically
// when Redis recovers; both probes below must pass, not just the first.
func TestModelRedisRateLimitFailOpenOnInfraError(t *testing.T) {
	require.NoError(t, i18n.Init())
	useDeadRedis(t)
	userID := 7300000 + int(modelRateLimitTestUsers.Add(1))
	handler := redisRateLimitHandler(60, 0, 1)
	router := gin.New()
	router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, handler, func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.MarkCompleted()
		common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		c.Status(http.StatusOK)
	})
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1001").Code)
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1001").Code)
}

// 预留式判定必须清理滑出窗口的陈旧条目：旧实现（LLen 统计 + LIndex 判定）
// 永远不会移除它们，低频用户的配额会被滞留条目永久占用。
func TestRedisSuccessWindowPrunesStaleAdmissions(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	ctx := context.Background()
	key := "rateLimit:model-prune"

	// RPush 使最新条目位于队头、最旧条目位于队尾（与 LPush 语义一致）
	fresh := time.Now().UTC().Add(-10 * time.Second).Format(modelRateLimitTimeFormat)
	stale := time.Now().UTC().Add(-2 * time.Minute).Format(modelRateLimitTimeFormat)
	_, err := redisClient.RPush(ctx, key, fresh, stale).Result()
	require.NoError(t, err)

	admission, allowed, err := reserveRedisSuccessSlot(ctx, redisClient, key, 2, 60)
	require.NoError(t, err)
	require.True(t, allowed, "清理超窗条目后窗口未满，必须放行")
	require.NotEmpty(t, admission)
	entries, err := redisClient.LRange(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	assert.Equal(t, []string{admission, fresh}, entries, "陈旧席位必须被清理，不得继续占用配额")
}

// 预留席位全值唯一，拒绝路径不写入，成功释放后精确移除自己的席位，
// 多次预留互不误伤（旧实现按值 LREM 会误删同毫秒的其他席位）。
func TestRedisSuccessWindowReservationAndReleaseRoundtrip(t *testing.T) {
	_, redisClient := useRateLimitMiniRedis(t)
	ctx := context.Background()
	key := "rateLimit:model-seat"

	first, allowed, err := reserveRedisSuccessSlot(ctx, redisClient, key, 2, 60)
	require.NoError(t, err)
	require.True(t, allowed)
	second, allowed, err := reserveRedisSuccessSlot(ctx, redisClient, key, 2, 60)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NotEqual(t, first, second, "同毫秒预留必须携带可区分的随机后缀")

	_, allowed, err = reserveRedisSuccessSlot(ctx, redisClient, key, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "满窗后应拒绝并且不新增条目")
	count, err := redisClient.LLen(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	releaseRedisSuccessSlot(ctx, redisClient, key, first)
	entries, err := redisClient.LRange(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	assert.Equal(t, []string{second}, entries, "释放必须精确移除对应席位")

	releaseRedisSuccessSlot(ctx, redisClient, key, second)
	count, err = redisClient.LLen(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

// 席位即会计：成功请求保留预留席位且不得重复补记（否则每次成功都被计
// 两次，配额迅速打满）；拒绝请求不得增长条目。
func TestModelRedisSuccessAdmissionIsRecordedOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	_, redisClient := useRateLimitMiniRedis(t)
	userID := 7400000 + int(modelRateLimitTestUsers.Add(1))
	router := gin.New()
	router.GET("/", func(c *gin.Context) { c.Set("id", userID) }, redisRateLimitHandler(60, 0, 2), func(c *gin.Context) {
		status := relaycommon.NewStreamStatus()
		status.MarkCompleted()
		common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		c.Status(http.StatusOK)
	})
	ctx := context.Background()
	successKey := fmt.Sprintf("rateLimit:%s:%d", ModelRequestRateLimitSuccessCountMark, userID)

	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
	count, err := redisClient.LLen(ctx, successKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "首次成功必须且只应留下一席")
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
	count, err = redisClient.LLen(ctx, successKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "第二次成功后满窗（max=2）")
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
	count, err = redisClient.LLen(ctx, successKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "拒绝路径不得增长条目")
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)
}
