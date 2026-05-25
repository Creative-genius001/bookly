package shops

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

type createShopRequest struct {
	Name            string `json:"name" binding:"required"`
	Slug            string `json:"slug"`
	Email           string `json:"email" binding:"required,email"`
	Phone           string `json:"phone" binding:"required"`
	Timezone        string `json:"timezone" binding:"required"`
	BarbingDuration int    `json:"barbing_duration"`
	CapacityPerSlot int    `json:"capacity_per_slot"`
}

type updateShopRequest struct {
	Name            *string `json:"name"`
	Slug            *string `json:"slug"`
	Email           *string `json:"email"`
	Phone           *string `json:"phone"`
	Timezone        *string `json:"timezone"`
	IsActive        *bool   `json:"is_active"`
	BarbingDuration *int    `json:"barbing_duration"`
	CapacityPerSlot *int    `json:"capacity_per_slot"`
}

type scheduleRequest struct {
	AllDays         bool   `json:"all_days"`
	ActiveDays      []int  `json:"active_days"`
	OpenTime        string `json:"open_time" binding:"required"`
	CloseTime       string `json:"close_time" binding:"required"`
	BarbingDuration *int   `json:"barbing_duration"`
	CapacityPerSlot *int   `json:"capacity_per_slot"`
}

type patchBusinessDayRequest struct {
	IsActive  *bool   `json:"is_active"`
	OpenTime  *string `json:"open_time"`
	CloseTime *string `json:"close_time"`
}

type blockedDateRequest struct {
	Date   string `json:"date" binding:"required"`
	Reason string `json:"reason"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateShop(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}

	var req createShopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	shop, err := h.service.CreateShop(c.Request.Context(), CreateShopInput{
		OwnerID:                ownerID,
		Name:                   req.Name,
		Slug:                   req.Slug,
		Email:                  req.Email,
		Phone:                  req.Phone,
		Timezone:               req.Timezone,
		BarbingDurationMinutes: req.BarbingDuration,
		CapacityPerSlot:        req.CapacityPerSlot,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}

	httpx.Created(c, shop)
}

func (h *Handler) GetShop(c *gin.Context) {
	shop, err := h.service.GetBySlug(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, shop)
}

func (h *Handler) UpdateShop(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req updateShopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	shop, err := h.service.UpdateShop(c.Request.Context(), UpdateShopInput{
		OwnerID:                ownerID,
		ShopID:                 shopID,
		Name:                   req.Name,
		Slug:                   req.Slug,
		Email:                  req.Email,
		Phone:                  req.Phone,
		Timezone:               req.Timezone,
		IsActive:               req.IsActive,
		BarbingDurationMinutes: req.BarbingDuration,
		CapacityPerSlot:        req.CapacityPerSlot,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}

	httpx.OK(c, shop)
}

func (h *Handler) UpsertBusinessDays(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req scheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	days, shop, err := h.service.UpsertBusinessDays(c.Request.Context(), ScheduleInput{
		OwnerID:                ownerID,
		ShopID:                 shopID,
		AllDays:                req.AllDays,
		ActiveDays:             req.ActiveDays,
		OpenTime:               req.OpenTime,
		CloseTime:              req.CloseTime,
		BarbingDurationMinutes: req.BarbingDuration,
		CapacityPerSlot:        req.CapacityPerSlot,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}

	httpx.OK(c, gin.H{"shop": shop, "business_days": days})
}

func (h *Handler) ListBusinessDays(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	days, err := h.service.ListBusinessDays(c.Request.Context(), ownerID, shopID)
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, days)
}

func (h *Handler) PatchBusinessDay(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	dayID, ok := parseUUIDParam(c, "dayId")
	if !ok {
		return
	}

	var req patchBusinessDayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	day, err := h.service.PatchBusinessDay(c.Request.Context(), PatchBusinessDayInput{
		OwnerID:   ownerID,
		ShopID:    shopID,
		DayID:     dayID,
		IsActive:  req.IsActive,
		OpenTime:  req.OpenTime,
		CloseTime: req.CloseTime,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, day)
}

func (h *Handler) AddBlockedDate(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req blockedDateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	blocked, err := h.service.AddBlockedDate(c.Request.Context(), BlockedDateInput{
		OwnerID: ownerID,
		ShopID:  shopID,
		Date:    req.Date,
		Reason:  req.Reason,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.Created(c, blocked)
}

func (h *Handler) ListBlockedDates(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	dates, err := h.service.ListBlockedDates(c.Request.Context(), ownerID, shopID)
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, dates)
}

func (h *Handler) DeleteBlockedDate(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, "authentication required")
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	blockedID, ok := parseUUIDParam(c, "blockedDateId")
	if !ok {
		return
	}

	if err := h.service.DeleteBlockedDate(c.Request.Context(), ownerID, shopID, blockedID); err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "blocked date deleted"})
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	value, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.BadRequest(c, name+" must be a valid UUID")
		return uuid.Nil, false
	}
	return value, true
}

func writeShopError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrShopNotFound), errors.Is(err, ErrBusinessDayAbsent):
		httpx.NotFound(c, err.Error())
	case errors.Is(err, ErrOwnerHasShop):
		httpx.Conflict(c, err.Error())
	case errors.Is(err, ErrForbiddenShop):
		httpx.Forbidden(c, err.Error())
	case errors.Is(err, ErrInvalidSchedule):
		httpx.BadRequest(c, "close_time must be after open_time")
	default:
		httpx.Error(c, http.StatusBadRequest, err.Error())
	}
}
