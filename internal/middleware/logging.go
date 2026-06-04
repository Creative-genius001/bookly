package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/logging"
	errorMap "barber-booking-backend/internal/utils/error"
)

func RequestLogger(base *slog.Logger) gin.HandlerFunc {
	if base == nil {
		base = slog.Default()
	}

	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}

		startedAt := time.Now()
		requestID := c.GetString("request_id")
		requestLogger := base.With("request_id", requestID)

		c.Request = c.Request.WithContext(logging.WithContext(c.Request.Context(), requestLogger))

		c.Next()

		status := c.Writer.Status()
		latency := time.Since(startedAt)

		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", c.FullPath(),
			"status", status,
			"latency_ms", elapsedMilliseconds(latency),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"response_bytes", c.Writer.Size(),
		}

		if userID, exists := c.Get("user_id"); exists {
			attrs = append(attrs, "user_id", userID)
		}
		if role, exists := c.Get("role"); exists {
			attrs = append(attrs, "role", role)
		}

		requestLogger.InfoContext(c.Request.Context(), "request completed", "data", c.Errors)
		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err

			var appErr *errorMap.AppError
			if errors.As(err, &appErr) {
				attrs = append(attrs,
					"error_code", appErr.Code.String(),
					"error_op", appErr.Op,
					"error_message", appErr.Message,
					"error_detail", appErr.Detail,
					"error_time", appErr.Time,
					"stack_trace", appErr.Stack,
				)
				if len(appErr.Fields) > 0 {
					attrs = append(attrs, "error_fields", appErr.Fields)
				}
			} else {
				attrs = append(attrs, "error", err.Error())
				attrs = append(attrs, "stack_trace", string(debug.Stack()))
			}
		}

		if status >= http.StatusBadRequest {
			requestLogger.ErrorContext(c.Request.Context(), "request completed", attrs...)
			return
		}

		requestLogger.InfoContext(c.Request.Context(), "request completed", attrs...)
	}
}

func Recovery(base *slog.Logger) gin.HandlerFunc {
	if base == nil {
		base = slog.Default()
	}

	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger := logging.FromContext(c.Request.Context())
				if logger == slog.Default() {
					logger = base
				}

				logger.ErrorContext(c.Request.Context(), "panic recovered",
					"panic", fmt.Sprint(recovered),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"client_ip", c.ClientIP(),
					"request_id", c.GetString("request_id"),
					"stack", string(debug.Stack()),
				)

				if c.Writer.Written() {
					c.Abort()
					return
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":  500,
					"error": "internal server error",
				})
			}
		}()

		c.Next()
	}
}

func elapsedMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
