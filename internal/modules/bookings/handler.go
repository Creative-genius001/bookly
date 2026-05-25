package bookings

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
)

type Handler struct {
	service *Service
}

type initiateRequest struct {
	SlotID uuid.UUID `json:"slot_id" binding:"required"`
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
	customerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}

	var req initiateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	result, err := h.service.Initiate(c.Request.Context(), customerID, req.SlotID)
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.Created(c, result)
}

func (h *Handler) Reschedule(c *gin.Context) {
	customerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}

	var req rescheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	booking, err := h.service.Reschedule(c.Request.Context(), customerID, req.Code, req.NewSlotID)
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.OK(c, booking)
}

func (h *Handler) Cancel(c *gin.Context) {
	customerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}

	var req cancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	booking, err := h.service.Cancel(c.Request.Context(), customerID, req.Code)
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.OK(c, booking)
}

func (h *Handler) GetByCode(c *gin.Context) {
	booking, err := h.service.GetByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.OK(c, booking)
}

func WriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrBookingNotFound), errors.Is(err, ErrPaymentNotFound):
		httpx.NotFound(c, err.Error())
	case errors.Is(err, ErrSlotNotBookable), errors.Is(err, ErrRescheduleNotAllowed), errors.Is(err, ErrCancelNotAllowed), errors.Is(err, ErrBookingWindow):
		httpx.BadRequest(c, err.Error())
	default:
		httpx.Error(c, http.StatusInternalServerError, err.Error())
	}
}
