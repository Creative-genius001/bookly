package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
	"barber-booking-backend/internal/models"
)

type Handler struct {
	service *Service
}

type signupRequest struct {
	Email    string          `json:"email" binding:"required,email"`
	Phone    string          `json:"phone" binding:"required"`
	Password string          `json:"password" binding:"required,min=8"`
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

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.Signup(c.Request.Context(), req.Email, req.Phone, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, ErrInvalidRole) {
			httpx.BadRequest(c, err.Error())
			return
		}
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			httpx.Conflict(c, "email or phone already exists")
			return
		}
		httpx.Error(c, http.StatusInternalServerError, "could not create account")
		return
	}

	httpx.Created(c, result)
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			httpx.Unauthorized(c, err.Error())
			return
		}
		httpx.Error(c, http.StatusInternalServerError, "could not login")
		return
	}

	httpx.OK(c, result)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidRefresh) {
			httpx.Unauthorized(c, err.Error())
			return
		}
		httpx.Error(c, http.StatusInternalServerError, "could not refresh token")
		return
	}

	httpx.OK(c, result)
}

func (h *Handler) Logout(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}

	var req logoutRequest
	_ = c.ShouldBindJSON(&req)

	if err := h.service.Logout(c.Request.Context(), userID.String(), req.RefreshToken); err != nil {
		httpx.Error(c, http.StatusInternalServerError, "could not logout")
		return
	}

	httpx.OK(c, gin.H{"message": "logged out"})
}
