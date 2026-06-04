package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

const (
	contextUserID = "user_id"
	contextRole   = "role"
)

func Auth(cfg config.JWTConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Auth Middleware", "authorization header is required"))
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Auth Middleware", "authorization header must be Bearer token"))
			return
		}

		claims, err := utils.ParseJWT(parts[1], cfg.Secret, utils.TokenTypeAccess)
		if err != nil {
			httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Auth Middleware", "invalid access token"))
			return
		}

		userID, _ := uuid.Parse(claims.UserID)
		c.Set(contextUserID, userID)
		c.Set(contextRole, claims.Role)
		c.Next()
	}
}

func RequireRole(roles ...models.UserRole) gin.HandlerFunc {
	allowed := make(map[models.UserRole]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		role, ok := CurrentRole(c)
		if !ok {
			httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "RequireRole Middleware", "authentication required"))
			return
		}
		if _, exists := allowed[role]; !exists {
			httpx.Forbidden(c, errorMap.New(errorMap.CodeForbidden, "RequireRole Middleware", "insufficient permissions"))
			return
		}
		c.Next()
	}
}

func CurrentUserID(c *gin.Context) (uuid.UUID, bool) {
	value, ok := c.Get(contextUserID)
	if !ok {
		return uuid.Nil, false
	}
	userID, ok := value.(uuid.UUID)
	return userID, ok
}

func CurrentRole(c *gin.Context) (models.UserRole, bool) {
	value, ok := c.Get(contextRole)
	if !ok {
		return "", false
	}
	role, ok := value.(models.UserRole)
	return role, ok
}
