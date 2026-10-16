package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

const (
	EmailVerificationRateLimitMark = "EV"
	EmailVerificationMaxRequests   = 2  // 30秒内最多2次
	EmailVerificationDuration      = 30 // 30秒时间窗口
)

func redisEmailVerificationRateLimiter(c *gin.Context) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(
		c.Request.Context(),
		redisIPRateLimitKey(EmailVerificationRateLimitMark, c.ClientIP()),
		EmailVerificationMaxRequests,
		EmailVerificationDuration,
	)
	if err != nil {
		// Redis 不可用时降级为内存限流（单机语义），但必须告警：
		// 静默降级会让多节点部署的限流形同虚设而不自知。
		logger.LogError(c.Request.Context(), common.LogText("email verification rate limit falling back to memory: %v", err))
		memoryEmailVerificationRateLimiter(c)
		return
	}
	if allowed {
		c.Next()
		return
	}

	waitSeconds := int64(EmailVerificationDuration)
	if ttlSeconds > 0 {
		waitSeconds = ttlSeconds
	}

	common.ApiErrorStatus(c, http.StatusTooManyRequests, common.NewMessage("Please wait {{seconds}} seconds before requesting another verification code.", map[string]any{"seconds": waitSeconds}))
	c.Abort()
}

func memoryEmailVerificationRateLimiter(c *gin.Context) {
	key := EmailVerificationRateLimitMark + ":" + c.ClientIP()

	if !inMemoryRateLimiter.Request(key, EmailVerificationMaxRequests, EmailVerificationDuration) {
		common.ApiErrorStatus(c, http.StatusTooManyRequests, common.NewMessage("Please wait before requesting another verification code."))
		c.Abort()
		return
	}

	c.Next()
}

func EmailVerificationRateLimit() gin.HandlerFunc {
	// Keep the fallback ready before requests arrive so a concurrent Redis
	// outage cannot race the in-memory limiter's first initialization.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		if common.RedisEnabled {
			redisEmailVerificationRateLimiter(c)
		} else {
			memoryEmailVerificationRateLimiter(c)
		}
	}
}
