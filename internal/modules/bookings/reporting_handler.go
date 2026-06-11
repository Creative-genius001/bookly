package bookings

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
	errorMap "barber-booking-backend/internal/utils/error"
)

func (h *Handler) GetBookingStatusStream(c *gin.Context) {
	h.service.BookingUpdateStream(c, c.Param("code"))
}

func (h *Handler) GetByCode(c *gin.Context) {
	booking, err := h.service.GetByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.OK(c, booking)
}

// ListForShop returns a paginated/searched list of a shop's bookings (owner).
func (h *Handler) ListForShop(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Booking Handler", "unauthorized user"))
		return
	}
	shopID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Booking Handler", "invalid shop ID"))
		return
	}

	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	result, err := h.service.ListForShop(c.Request.Context(), ownerID, shopID, ListBookingsParams{
		Status:   c.Query("status"),
		Search:   c.Query("search"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		WriteError(c, err)
		return
	}
	httpx.OK(c, result)
}

// Analytics returns aggregate booking/revenue metrics (owner dashboard).
func (h *Handler) Analytics(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Booking Handler", "unauthorized user"))
		return
	}
	shopID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Error(c, http.StatusBadRequest, errorMap.New(errorMap.CodeInvalidInput, "Booking Handler", "invalid shop ID").Error())
		return
	}

	summary, err := h.service.Analytics(c.Request.Context(), ownerID, shopID, c.Query("range"))
	if err != nil {
		WriteError(c, err)
		return
	}
	// Owner-specific and moderately expensive — let the browser hold it briefly.
	httpx.CachePrivate(c, 60)
	httpx.OK(c, summary)
}
