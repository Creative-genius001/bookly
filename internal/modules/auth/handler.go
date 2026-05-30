package auth

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
	"barber-booking-backend/internal/models"
	errorMap "barber-booking-backend/internal/utils/error"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

type signupRequest struct {
	Email    string          `json:"email" binding:"required"`
	Phone    string          `json:"phone" binding:"required"`
	Password string          `json:"password" binding:"required"`
	Role     models.UserRole `json:"role" binding:"required"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger.With("component", "auth_service"),
	}
}

func (h *Handler) Signup(c *gin.Context) {
	ctx := c.Request.Context()

	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Signup Handler", "request body is invalid"))
		return
	}

	result, err := h.service.Signup(ctx, req.Email, req.Phone, req.Password, req.Role)
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) {
			switch appErr.Code {
			case errorMap.CodeInvalidInput:
				httpx.BadRequest(c, appErr)
				return
			case errorMap.CodeAlreadyExists:
				httpx.Conflict(c, appErr)
				return
			default:
				httpx.InternalServerError(c, appErr)
				return
			}
		}
	}

	httpx.Created(c, result)
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Login Handler", "request body is invalid"))
		return
	}

	result, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) {
			switch appErr.Code {
			case errorMap.CodeInvalidInput:
				httpx.BadRequest(c, appErr)
				return
			case errorMap.CodeNotFound:
				httpx.BadRequest(c, appErr)
				return
			default:
				httpx.InternalServerError(c, appErr)
				return
			}
		}
	}

	httpx.OK(c, result)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Refresh Handler", "request body is invalid"))
		return
	}

	result, err := h.service.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) {
			switch appErr.Code {
			case errorMap.CodeInvalidInput:
				httpx.BadRequest(c, appErr)
				return
			case errorMap.CodeNotFound:
				httpx.BadRequest(c, appErr)
				return
			default:
				httpx.InternalServerError(c, appErr)
				return
			}
		}
	}

	httpx.OK(c, result)
}

func (h *Handler) Logout(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Logout Handler", "invalid user"))
		return
	}

	var req logoutRequest
	_ = c.ShouldBindJSON(&req)

	if err := h.service.Logout(c.Request.Context(), userID.String(), req.RefreshToken); err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) {
			httpx.InternalServerError(c, appErr)
			return
		}
		httpx.InternalServerError(c, errorMap.New(errorMap.CodeInternal, "Logout Handler", "failed to logout"))
		return
	}

	httpx.OK(c, gin.H{"message": "logged out"})
}
