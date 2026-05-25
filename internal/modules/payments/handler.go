package payments

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
	"barber-booking-backend/internal/modules/bookings"
)

type Handler struct {
	bookings *bookings.Service
}

type initRequest struct {
	BookingCode string `json:"booking_code" binding:"required"`
}

func NewHandler(bookingsService *bookings.Service) *Handler {
	return &Handler{bookings: bookingsService}
}

func (h *Handler) Init(c *gin.Context) {
	customerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}

	var req initRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	result, err := h.bookings.InitializePayment(c.Request.Context(), customerID, req.BookingCode)
	if err != nil {
		bookings.WriteError(c, err)
		return
	}
	httpx.OK(c, result)
}

func (h *Handler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.BadRequest(c, "could not read webhook body")
		return
	}

	if err := h.bookings.HandlePaystackWebhook(c.Request.Context(), body, c.GetHeader("X-Paystack-Signature")); err != nil {
		httpx.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	httpx.OK(c, gin.H{"received": true})
}
