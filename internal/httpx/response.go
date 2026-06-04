package httpx

import (
	"net/http"

	errorMap "barber-booking-backend/internal/utils/error"

	"github.com/gin-gonic/gin"
)

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

func Error(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message, "code": status})
}

func InternalServerError(c *gin.Context, appErr *errorMap.AppError) {
	if appErr != nil {
		c.Error(appErr)
	}
	Error(c, http.StatusInternalServerError, "internal server error")
}

func BadRequest(c *gin.Context, appErr *errorMap.AppError) {
	c.Error(appErr)
	Error(c, http.StatusBadRequest, appErr.Message)
}

func Unauthorized(c *gin.Context, appErr *errorMap.AppError) {
	c.Error(appErr)
	Error(c, http.StatusUnauthorized, appErr.Message)
}

func Forbidden(c *gin.Context, appErr *errorMap.AppError) {
	c.Error(appErr)
	Error(c, http.StatusForbidden, appErr.Message)
}

func NotFound(c *gin.Context, appErr *errorMap.AppError) {
	c.Error(appErr)
	Error(c, http.StatusNotFound, appErr.Message)
}

func Conflict(c *gin.Context, appErr *errorMap.AppError) {
	c.Error(appErr)
	Error(c, http.StatusConflict, appErr.Message)
}
