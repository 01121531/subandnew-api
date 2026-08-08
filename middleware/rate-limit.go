package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/gin-gonic/gin"
)

const rateLimitTimeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter common.InMemoryRateLimiter

func redisRateLimiter(c *gin.Context, maximum int, duration int64, prefix string) {
	ctx := context.Background()
	key := "rateLimit:" + prefix + c.ClientIP()
	count, err := common.RDB.LLen(ctx, key).Result()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if count < int64(maximum) {
		common.RDB.LPush(ctx, key, time.Now().Format(rateLimitTimeFormat))
		common.RDB.Expire(ctx, key, common.RateLimitKeyExpirationDuration)
		return
	}
	oldestText, _ := common.RDB.LIndex(ctx, key, -1).Result()
	oldest, err := time.Parse(rateLimitTimeFormat, oldestText)
	if err != nil {
		fmt.Println(err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if int64(time.Since(oldest).Seconds()) < duration {
		common.RDB.Expire(ctx, key, common.RateLimitKeyExpirationDuration)
		c.AbortWithStatus(http.StatusTooManyRequests)
		return
	}
	common.RDB.LPush(ctx, key, time.Now().Format(rateLimitTimeFormat))
	common.RDB.LTrim(ctx, key, 0, int64(maximum-1))
	common.RDB.Expire(ctx, key, common.RateLimitKeyExpirationDuration)
}

func rateLimitFactory(maximum int, duration int64, prefix string) gin.HandlerFunc {
	if common.RedisEnabled {
		return func(c *gin.Context) { redisRateLimiter(c, maximum, duration, prefix) }
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		if !inMemoryRateLimiter.Request(prefix+c.ClientIP(), maximum, duration) {
			c.AbortWithStatus(http.StatusTooManyRequests)
		}
	}
}

func GlobalWebRateLimit() gin.HandlerFunc {
	if !common.GlobalWebRateLimitEnable {
		return func(c *gin.Context) { c.Next() }
	}
	return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
}

func GlobalAPIRateLimit() gin.HandlerFunc {
	if !common.GlobalApiRateLimitEnable {
		return func(c *gin.Context) { c.Next() }
	}
	return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
}

func CriticalRateLimit() gin.HandlerFunc {
	if !common.CriticalRateLimitEnable {
		return func(c *gin.Context) { c.Next() }
	}
	return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
}
