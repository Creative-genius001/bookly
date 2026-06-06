package shops

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"barber-booking-backend/internal/httpx"
	"barber-booking-backend/internal/middleware"
	errorMap "barber-booking-backend/internal/utils/error"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
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

type shopServiceRequest struct {
	Name            string `json:"name" binding:"required"`
	Description     string `json:"description"`
	Price           int    `json:"price" binding:"required"`
	BarbingDuration int    `json:"barbing_duration" binding:"required"`
}

type AddServiceInput struct {
	OwnerID                uuid.UUID
	ShopID                 uuid.UUID
	Name                   string
	Description            string
	Price                  int
	BarbingDurationMinutes int
}

type UpdateServiceInput struct {
	ShopID                 uuid.UUID
	ServiceID              uuid.UUID
	OwnerID                uuid.UUID
	Name                   string
	IsActive               bool
	Description            string
	Price                  int
	BarbingDurationMinutes int
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) CreateShop(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}

	currentRole, ok := middleware.CurrentRole(c)
	if !ok || currentRole != "owner" {
		httpx.Forbidden(c, errorMap.New(errorMap.CodeForbidden, "Shop Handler", "only users with owner role can create shops"))
		return
	}

	var req createShopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req updateShopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req scheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
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
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req blockedDateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
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
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
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

func (h *Handler) AddService(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid shop ID"))
		return
	}

	var req shopServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
		return
	}

	service, err := h.service.AddService(c.Request.Context(), AddServiceInput{
		OwnerID:                ownerID,
		ShopID:                 shopID,
		Name:                   req.Name,
		Description:            req.Description,
		Price:                  req.Price,
		BarbingDurationMinutes: req.BarbingDuration,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.Created(c, service)

}

func (h *Handler) ListServices(c *gin.Context) {
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid shop ID"))
		return
	}

	services, err := h.service.ListServices(c.Request.Context(), shopID)
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, services)
}

func (h *Handler) DeleteService(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}
	serviceID, ok := parseUUIDParam(c, "id")
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid service ID"))
		return
	}

	if err := h.service.DeleteService(c.Request.Context(), ownerID, serviceID); err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "service deleted"})
}

func (h *Handler) GetService(c *gin.Context) {
	serviceID, ok := parseUUIDParam(c, "id")
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid service ID"))
		return
	}

	service, err := h.service.GetService(c.Request.Context(), serviceID)
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, service)
}

func (h *Handler) UpdateService(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Unauthorized(c, errorMap.New(errorMap.CodeUnauthorized, "Shop Handler", "unauthorized user"))
		return
	}
	shopID, ok := parseUUIDParam(c, "id")
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid shop ID"))
		return
	}
	serviceID, ok := parseUUIDParam(c, "serviceId")
	if !ok {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid service ID"))
		return
	}

	var req shopServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, errorMap.New(errorMap.CodeInvalidInput, "Shop Handler", "invalid request body"))
		return
	}

	service, err := h.service.UpdateService(c.Request.Context(), UpdateServiceInput{
		ShopID:                 shopID,
		ServiceID:              serviceID,
		OwnerID:                ownerID,
		Name:                   req.Name,
		Description:            req.Description,
		Price:                  req.Price,
		BarbingDurationMinutes: req.BarbingDuration,
	})
	if err != nil {
		writeShopError(c, err)
		return
	}
	httpx.OK(c, service)
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	value, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, false
	}
	return value, true
}

func writeShopError(c *gin.Context, err error) {
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
