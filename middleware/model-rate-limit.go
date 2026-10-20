package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
	modelRateLimitTimeFormat              = "2006-01-02T15:04:05.000Z"
)

// redisSuccessWindowScript 是「成功数滑动窗口」的一次性原子操作：
// 清理超窗条目 → 按需预留席位 → 给出判定。
//
// 为什么必须是单次 Lua 而不是两次往返：旧实现用 LLen 统计、再 LIndex(-1)
// 取最老条目判定，两次往返之间其他请求的预留/清除可能插入，导致判定依据
// 的列表状态失真；占用统计又发生在请求处理完成之后，判定完全不消耗，并
// 发突发可以成批越过上限。Redis 串行执行脚本，「预留」与「判定」在同一
// 次调用内完成，并发的每个判定读到的窗口状态严格递增。
//
// 条目形态兼容：历史数据是纯时间戳字符串，预留席位是「时间戳:随机值」。
// 两类条目都以 24 字符 UTC 毫秒时间戳开头，定长格式下字典序即时间序，
// 过期清理（LINDEX 尾部比较）对两种形态统一成立。
var redisSuccessWindowScript = redis.NewScript(`
local key = KEYS[1]
local maxCount = tonumber(ARGV[1])
local cutoff = ARGV[2]
local admission = ARGV[3]
local ttlSeconds = tonumber(ARGV[4])

-- LPush 保持最新条目在队头，队尾即最老条目；
-- 值小于窗口起点（cutoff）的条目已完全滑出时间窗，安全丢弃。
while true do
  local oldest = redis.call('LINDEX', key, -1)
  if not oldest or oldest >= cutoff then
    break
  end
  redis.call('RPOP', key)
end

-- 活跃窗口在华每次检查时续期，低频用户不需要跨 TTL 重建窗口。
if ttlSeconds > 0 then
  redis.call('EXPIRE', key, ttlSeconds)
end

-- 窗口未满：预留席位（即本次请求的会计记录）并放行；满员则拒绝。
local length = redis.call('LLEN', key)
if length < maxCount then
  redis.call('LPUSH', key, admission)
  return 1
end
return 0
`)

// admissionNonce 为席位值生成随机后缀。时间戳仅毫秒分辨率，同一毫秒内
// 的两次预留若不追加随机值，失败释放时 LREM 会按值误删他人的席位。
func admissionNonce() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败降低为纳秒时间戳；同毫秒碰撞概率极小，
		// 误删一个席位只相当于该用户少计一次成功请求，可接受。
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(buf[:])
}

// reserveRedisSuccessSlot 原子地完成「检查 + 预留成功数席位」：
// 返回 allowed=true 时键内已预置一个席位（值为返回的令牌），请求以失败
// 收场时必须调用 releaseRedisSuccessSlot 归还；成功时席位自然留存为成功
// 记录，无需再补记。maxCount 为 0 表示不限流，不做任何 Redis 操作。
func reserveRedisSuccessSlot(ctx context.Context, rdb *redis.Client, key string, maxCount int, duration int64) (admission string, allowed bool, err error) {
	if maxCount == 0 {
		return "", true, nil
	}

	now := time.Now().UTC()
	// cutoff 为窗口起点的时间戳字符串，仅与条目值做定长前缀比较
	cutoff := now.Add(-time.Duration(duration) * time.Second).Format(modelRateLimitTimeFormat)
	admission = now.Format(modelRateLimitTimeFormat) + ":" + admissionNonce()

	allowedInt, err := redisSuccessWindowScript.Run(ctx, rdb, []string{key}, maxCount, cutoff, admission, duration).Int()
	if err != nil {
		return "", false, err
	}
	if allowedInt != 1 {
		// 拒绝路径未预置席位，令牌必须为空，调用方据此判断是否需要释放
		return "", false, nil
	}
	return admission, true, nil
}

// releaseRedisSuccessSlot 归还一次预留席位。令牌携带随机后缀、全值唯一，
// LREM count=1 只会精确移除本次预留的条目。空令牌（不限流路径或历史
// fail-open 场景）直接跳过，保持该函数可无条件调用。
func releaseRedisSuccessSlot(ctx context.Context, rdb *redis.Client, key string, admission string) {
	if admission == "" {
		return
	}
	if _, err := rdb.LRem(ctx, key, 1, admission).Result(); err != nil {
		common.SysLog(common.LogText("failed to release model rate limit reservation: %s", err.Error()))
	}
}

// 记录Redis请求（仅降级路径使用：预留检查 fail-open 时改为「请求成功后补记」的旧会计形态）
func recordRedisRequest(ctx context.Context, rdb *redis.Client, key string, maxCount int) {
	// 如果maxCount为0，不记录请求
	if maxCount == 0 {
		return
	}

	now := time.Now().UTC().Format(modelRateLimitTimeFormat)
	_, err := rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.LPush(ctx, key, now)
		pipe.LTrim(ctx, key, 0, int64(maxCount-1))
		pipe.Expire(ctx, key, time.Duration(setting.ModelRequestRateLimitDurationMinutes)*time.Minute)
		return nil
	})
	if err != nil {
		common.SysLog(common.LogText("failed to record model request for rate limiting: %s", err.Error()))
	}
}

// Redis限流处理器
func redisRateLimitHandler(duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		userId := strconv.Itoa(c.GetInt("id"))
		ctx := context.Background()
		rdb := common.RDB

		// 1. 检查并预留成功请求数席位（检查即消耗，消除突发并发越过上限）。
		// 基础设施故障 fail-open：逻辑同主限流器，误杀正常请求比漏过超限
		// 请求代价更高；此时无席位可释放，回退为「请求成功后补记」的会计。
		successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userId)
		admission, allowed, err := reserveRedisSuccessSlot(ctx, rdb, successKey, successMaxCount, duration)
		if err != nil {
			logger.LogError(ctx, common.LogText("model rate limit fail-open: %s", err.Error()))
			c.Next()
			if modelRequestSucceeded(c) {
				recordRedisRequest(ctx, rdb, successKey, successMaxCount)
			}
			return
		}
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": successMaxCount}))
			return
		}

		//2.检查总请求数限制并记录总请求（当totalMaxCount为0时会自动跳过，使用令牌桶限流器
		if totalMaxCount > 0 {
			totalKey := fmt.Sprintf("rateLimit:%s", userId)
			// 初始化
			tb := limiter.New(ctx, rdb)
			allowed, err = tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(rateLimitCapacity(totalMaxCount, duration)),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)

			if err != nil {
				logger.LogError(ctx, common.LogText("model rate limit fail-open (total): %s", err.Error()))
				c.Next()
				if !modelRequestSucceeded(c) {
					releaseRedisSuccessSlot(ctx, rdb, successKey, admission)
				}
				return
			}

			if !allowed {
				// 请求未进入处理流程，成功数席位必须归还
				releaseRedisSuccessSlot(ctx, rdb, successKey, admission)
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": totalMaxCount}))
				return
			}
		}

		// 4. 处理请求
		c.Next()

		// 5. 席位即会计：成功请求保留预留席位成为成功记录，失败请求即时
		// 释放；进程崩溃时的席位泄漏最多存活一个窗口 TTL，不再扩散。
		if !modelRequestSucceeded(c) {
			releaseRedisSuccessSlot(ctx, rdb, successKey, admission)
		}
	}
}

// 内存限流处理器
func memoryRateLimitHandler(duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	return func(c *gin.Context) {
		userId := strconv.Itoa(c.GetInt("id"))
		totalKey := ModelRequestRateLimitCountMark + userId
		successKey := ModelRequestRateLimitSuccessCountMark + userId

		// 1. 检查总请求数限制（当totalMaxCount为0时跳过）
		if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": totalMaxCount}))
			return
		}

		var reservation *common.RateLimitReservation
		if successMaxCount > 0 {
			reservation = inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
			if reservation == nil {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitReached, map[string]any{"Minutes": setting.ModelRequestRateLimitDurationMinutes, "Max": successMaxCount}))
				return
			}
			defer reservation.Complete(false)
		}

		// 3. 处理请求
		c.Next()

		// 4. 如果请求成功，记录到实际的成功请求计数中
		reservation.Complete(modelRequestSucceeded(c))
	}
}

func modelRequestSucceeded(c *gin.Context) bool {
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
	return c.Writer.Status() < 400 && !status.ResponseFailed()
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 在每个请求时检查是否启用限流
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		// 计算限流参数
		duration := rateLimitDurationSeconds(setting.ModelRequestRateLimitDurationMinutes)
		totalMaxCount := setting.ModelRequestRateLimitCount
		successMaxCount := setting.ModelRequestRateLimitSuccessCount

		// 获取分组
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		//获取分组的限流配置
		groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
		}

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(duration, totalMaxCount, successMaxCount)(c)
		} else {
			memoryRateLimitHandler(duration, totalMaxCount, successMaxCount)(c)
		}
	}
}

func rateLimitDurationSeconds(durationMinutes int) int64 {
	if durationMinutes <= 0 {
		return 0
	}
	minutes := int64(durationMinutes)
	if minutes > math.MaxInt64/60 {
		return math.MaxInt64
	}
	return minutes * 60
}

func rateLimitCapacity(count int, durationSeconds int64) int64 {
	if count <= 0 || durationSeconds <= 0 {
		return 0
	}
	c := int64(count)
	if c > math.MaxInt64/durationSeconds {
		return math.MaxInt64
	}
	return c * durationSeconds
}
