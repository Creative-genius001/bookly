package payments

import (
	"io"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/modules/bookings"
	errorMap "barber-booking-backend/internal/utils/error"
)

type Handler struct {
	bookings *bookings.Service
}

type initRequest struct {
	BookingCode       string `json:"booking_code" binding:"required"`
	PaymmentReference string `json:"payment_reference" binding:"required"`
}

func NewHandler(bookingsService *bookings.Service) *Handler {
	return &Handler{bookings: bookingsService}
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
	_, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Payment Handler", "could not read webhook body"))
		return
	}

	// if err := h.bookings.HandlePaystackWebhook(c.Request.Context(), body, c.GetHeader("X-Paystack-Signature")); err != nil {
	// 	httpx.InternalServerError(c, errorMap.New(errorMap.CodeInvalidInput, "Payment Handler", err.Error()))
	// 	return
	// }

	httpx.OK(c, gin.H{"received": true})
}
