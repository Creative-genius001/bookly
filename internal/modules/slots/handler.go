package slots

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"barber-booking-backend/internal/httpx"
	errorMap "barber-booking-backend/internal/utils/error"
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
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Booking Handler", "date query parameter is required"))
		return
	}

	result, err := h.service.AvailabilityForDate(c.Request.Context(), c.Param("id"), date, time.Now())
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) {
			switch appErr.Code {
			case errorMap.CodeNotFound:
				httpx.NotFound(c, appErr)
			case errorMap.CodeInvalidInput:
				httpx.BadRequest(c, appErr)
			case errorMap.CodeInternal:
				httpx.InternalServerError(c, appErr)
			default:
				httpx.Error(c, http.StatusBadRequest, errorMap.New(errorMap.CodeInternal, "Booking Handler", "an unexpected error occurred").Error())
			}
			return
		}
	}

	httpx.OK(c, result)
}
