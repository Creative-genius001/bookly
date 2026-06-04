package bookings

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"barber-booking-backend/internal/httpx"
	errorMap "barber-booking-backend/internal/utils/error"
)

type Handler struct {
	service *Service
}

type initiateRequest struct {
	ServiceID     uuid.UUID `json:"service_id" binding:"required"`
	CustomerName  string    `json:"customer_name" binding:"required"`
	CustomerEmail string    `json:"customer_email" binding:"required,email"`
	StartTime     time.Time `json:"start_time" binding:"required"`
	EndTime       time.Time `json:"end_time" binding:"required"`
}

type rescheduleRequest struct {
	Code      string    `json:"code" binding:"required"`
	NewSlotID uuid.UUID `json:"new_slot_id" binding:"required"`
}

type cancelRequest struct {
	Code string `json:"code" binding:"required"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Initiate(c *gin.Context) {
	var payload initiateRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Booking Handler", "request body is invalid"))
		return
	}

	result, err := h.service.Initiate(c.Request.Context(), payload)
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.Created(c, result)
}

// func (h *Handler) Reschedule(c *gin.Context) {
// 	customerID, ok := middleware.CurrentUserID(c)
// 	if !ok {
// 		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Booking Handler", "unauthorized user"))
// 		return
// 	}

// 	var req rescheduleRequest
// 	if err := c.ShouldBindJSON(&req); err != nil {
// 		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Booking Handler", "request body is invalid"))
// 		return
// 	}

// 	booking, err := h.service.Reschedule(c.Request.Context(), customerID, req.Code, req.NewSlotID)
// 	if err != nil {
// 		WriteError(c, err)
// 		return
// 	}
// 	httpx.OK(c, booking)
// }

// func (h *Handler) Cancel(c *gin.Context) {
// 	customerID, ok := middleware.CurrentUserID(c)
// 	if !ok {
// 		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Booking Handler", "unauthorized user"))
// 		return
// 	}

// 	var req cancelRequest
// 	if err := c.ShouldBindJSON(&req); err != nil {
// 		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Booking Handler", "request body is invalid"))
// 		return
// 	}

// 	booking, err := h.service.Cancel(c.Request.Context(), customerID, req.Code)
// 	if err != nil {
// 		WriteError(c, err)
// 		return
// 	}
// 	httpx.OK(c, booking)
// }

// func (h *Handler) GetByCode(c *gin.Context) {
// 	booking, err := h.service.GetByCode(c.Request.Context(), c.Param("code"))
// 	if err != nil {
// 		WriteError(c, err)
// 		return
// 	}
// 	httpx.OK(c, booking)
// }

func WriteError(c *gin.Context, err error) {
	var appErr *errorMap.AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case errorMap.CodeNotFound:
			httpx.NotFound(c, appErr)
		case errorMap.CodeAlreadyExists:
			httpx.Conflict(c, appErr)
		case errorMap.CodeForbidden:
			httpx.Forbidden(c, appErr)
		case errorMap.CodeInvalidInput:
			httpx.BadRequest(c, appErr)
		case errorMap.CodeUnauthorized:
			httpx.Unauthorized(c, appErr)
		case errorMap.CodeInternal:
			httpx.InternalServerError(c, appErr)
		default:
			httpx.Error(c, http.StatusInternalServerError, errorMap.New(errorMap.CodeInternal, "Shop Handler", "an unexpected error occurred").Error())
		}
		return
	}

	httpx.Error(c, http.StatusInternalServerError, errorMap.New(errorMap.CodeInternal, "Shop Handler", "an unexpected error occurred").Error())
}
