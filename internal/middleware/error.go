package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func ErrorHandler(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		err := c.Errors.Last().Err

		logger.Error("request failed",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"code", c.Writer.Status(),
			"error", err,
		)
	}
}
