package slots

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetSlots(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		httpx.BadRequest(c, "date query parameter is required")
		return
	}

	result, err := h.service.SlotsForDate(c.Request.Context(), c.Param("id"), date, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, ErrShopUnavailable):
			httpx.NotFound(c, err.Error())
		case errors.Is(err, ErrSlotWindow):
			httpx.BadRequest(c, "date must be within the rolling 14-day booking window")
		default:
			httpx.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	httpx.OK(c, result)
}
