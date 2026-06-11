package payments

import (
	"io"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/modules/bookings"
	"barber-booking-backend/internal/modules/payouts"
	errorMap "barber-booking-backend/internal/utils/error"
)

type Handler struct {
	bookings *bookings.Service
	payouts  *payouts.Service
}

type initRequest struct {
	BookingCode       string `json:"booking_code" binding:"required"`
	PaymmentReference string `json:"payment_reference" binding:"required"`
}

func NewHandler(bookingsService *bookings.Service, payoutsService *payouts.Service) *Handler {
	return &Handler{bookings: bookingsService, payouts: payoutsService}
}

func (h *Handler) Init(c *gin.Context) {

	var req initRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Payment Handler", "request body is invalid"))
		return
	}

	result, err := h.bookings.InitializePayment(c.Request.Context(), req.BookingCode, req.PaymmentReference)
	if err != nil {
		bookings.WriteError(c, err)
		return
	}
	httpx.OK(c, result)
}

func (h *Handler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Payment Handler", "could not read webhook body"))
		return
	}

	sig := c.GetHeader("X-Paystack-Signature")

	if err := h.bookings.HandleWebhook(c.Request.Context(), body, sig); err != nil {
		bookings.WriteError(c, err)
		return
	}
	if err := h.payouts.HandleWebhook(c.Request.Context(), body, sig); err != nil {
		bookings.WriteError(c, err)
		return
	}

	httpx.OK(c, gin.H{"received": true})
}
