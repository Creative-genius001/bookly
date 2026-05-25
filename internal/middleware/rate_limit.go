package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func RateLimit(client *redis.Client, limitPerMinute int) gin.HandlerFunc {
	if limitPerMinute <= 0 {
		limitPerMinute = 120
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now().UTC()
		key := fmt.Sprintf("rate:%s:%s", ip, now.Format("200601021504"))

		count, err := client.Incr(c.Request.Context(), key).Result()
		if err != nil {
			c.Next()
			return
		}
		if count == 1 {
			_ = client.Expire(c.Request.Context(), key, time.Minute).Err()
		}
		if count > int64(limitPerMinute) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}

		c.Next()
	}
}
