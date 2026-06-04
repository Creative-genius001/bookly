package shops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

var (
	ErrShopNotFound      = errors.New("shop not found")
	ErrOwnerHasShop      = errors.New("owner can only have one shop")
	ErrForbiddenShop     = errors.New("shop does not belong to owner")
	ErrInvalidSchedule   = errors.New("invalid schedule")
	ErrBusinessDayAbsent = errors.New("business day not found")
)

type Service struct {
	db     *gorm.DB
	logger *slog.Logger
}

type CreateShopInput struct {
	OwnerID                uuid.UUID
	Name                   string
	Slug                   string
	Email                  string
	Phone                  string
	Timezone               string
	BarbingDurationMinutes int
	CapacityPerSlot        int
}

type UpdateShopInput struct {
	OwnerID                uuid.UUID
	ShopID                 uuid.UUID
	Name                   *string
	Slug                   *string
	Email                  *string
	Phone                  *string
	Timezone               *string
	IsActive               *bool
	BarbingDurationMinutes *int
	CapacityPerSlot        *int
}

type ScheduleInput struct {
	OwnerID                uuid.UUID
	ShopID                 uuid.UUID
	AllDays                bool
	ActiveDays             []int
	OpenTime               string
	CloseTime              string
	BarbingDurationMinutes *int
	CapacityPerSlot        *int
}

type PatchBusinessDayInput struct {
	OwnerID   uuid.UUID
	ShopID    uuid.UUID
	DayID     uuid.UUID
	IsActive  *bool
	OpenTime  *string
	CloseTime *string
}

type BlockedDateInput struct {
	OwnerID uuid.UUID
	ShopID  uuid.UUID
	Date    string
	Reason  string
}

func NewService(db *gorm.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

func (s *Service) CreateShop(ctx context.Context, input CreateShopInput) (models.Shop, error) {
	if err := validateTimezone(input.Timezone); err != nil {
		return models.Shop{}, err
	}
	if input.BarbingDurationMinutes == 0 {
		input.BarbingDurationMinutes = 60
	}
	if input.CapacityPerSlot == 0 {
		input.CapacityPerSlot = 1
	}
	if err := validateShopTiming(input.BarbingDurationMinutes); err != nil {
		return models.Shop{}, err
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Shop{}).Where("owner_id = ?", input.OwnerID).Count(&count).Error; err != nil {
		return models.Shop{}, err
	}
	if count > 0 {
		return models.Shop{}, errorMap.New(errorMap.CodeInvalidInput, "Create Shop", ErrOwnerHasShop.Error())
	}

	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		slug = utils.Slugify(input.Name)
	} else {
		slug = utils.Slugify(slug)
	}
	slug = s.uniqueSlug(ctx, slug)

	shop := models.Shop{
		OwnerID:                input.OwnerID,
		Name:                   strings.TrimSpace(input.Name),
		Slug:                   slug,
		Email:                  strings.ToLower(strings.TrimSpace(input.Email)),
		Phone:                  strings.TrimSpace(input.Phone),
		Timezone:               input.Timezone,
		IsActive:               true,
		BarbingDurationMinutes: input.BarbingDurationMinutes,
		CapacityPerSlot:        input.CapacityPerSlot,
	}

	if err := s.db.WithContext(ctx).Create(&shop).Error; err != nil {
		return models.Shop{}, err
	}
	return shop, nil
}

func (s *Service) GetBySlug(ctx context.Context, slug string) (models.Shop, error) {
	var shop models.Shop
	err := s.db.WithContext(ctx).Where("slug = ?", utils.Slugify(slug)).First(&shop).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Shop{}, errorMap.New(errorMap.CodeNotFound, "Get Shop", ErrShopNotFound.Error())
	}
	return shop, err
}

func (s *Service) UpdateShop(ctx context.Context, input UpdateShopInput) (models.Shop, error) {
	shop, err := s.findOwnedShop(ctx, input.OwnerID, input.ShopID)
	if err != nil {
		return models.Shop{}, err
	}

	updates := map[string]interface{}{}
	if input.Name != nil {
		updates["name"] = strings.TrimSpace(*input.Name)
	}
	if input.Slug != nil {
		updates["slug"] = s.uniqueSlugExcluding(ctx, utils.Slugify(*input.Slug), shop.ID)
	}
	if input.Email != nil {
		updates["email"] = strings.ToLower(strings.TrimSpace(*input.Email))
	}
	if input.Phone != nil {
		updates["phone"] = strings.TrimSpace(*input.Phone)
	}
	if input.Timezone != nil {
		if err := validateTimezone(*input.Timezone); err != nil {
			return models.Shop{}, err
		}
		updates["timezone"] = *input.Timezone
	}
	if input.IsActive != nil {
		updates["is_active"] = *input.IsActive
	}
	if input.BarbingDurationMinutes != nil {
		if err := validateShopTiming(*input.BarbingDurationMinutes); err != nil {
			return models.Shop{}, err
		}
		updates["barbing_duration_minutes"] = *input.BarbingDurationMinutes
	}
	if input.CapacityPerSlot != nil {
		if err := validateShopTiming(shop.BarbingDurationMinutes); err != nil {
			return models.Shop{}, err
		}
		updates["capacity_per_slot"] = *input.CapacityPerSlot
	}

	if len(updates) > 0 {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&shop).Updates(updates).Error; err != nil {
				return errorMap.Wrap(err, errorMap.CodeInternal, "Update Shop Service", "Could not update shop details")
			}
			if input.Timezone != nil || input.BarbingDurationMinutes != nil || input.CapacityPerSlot != nil {
				capacity := shop.CapacityPerSlot
				if input.CapacityPerSlot != nil {
					capacity = *input.CapacityPerSlot
				}
				return refreshFutureSlots(tx, shop.ID, capacity, input.Timezone != nil || input.BarbingDurationMinutes != nil)
			}
			return nil
		})
		if err != nil {
			return models.Shop{}, err
		}
		if err := s.db.WithContext(ctx).First(&shop, "id = ?", shop.ID).Error; err != nil {
			return models.Shop{}, err
		}
	}
	return shop, nil
}

func (s *Service) UpsertBusinessDays(ctx context.Context, input ScheduleInput) ([]models.BusinessDay, models.Shop, error) {
	shop, err := s.findOwnedShop(ctx, input.OwnerID, input.ShopID)
	if err != nil {
		return nil, models.Shop{}, err
	}

	openTime, closeTime, err := normalizeSchedule(input.OpenTime, input.CloseTime)
	if err != nil {
		return nil, models.Shop{}, err
	}

	activeSet, err := activeDaySet(input.AllDays, input.ActiveDays)
	if err != nil {
		return nil, models.Shop{}, err
	}

	if input.BarbingDurationMinutes != nil {
		if err := validateShopTiming(*input.BarbingDurationMinutes); err != nil {
			return nil, models.Shop{}, err
		}
		shop.BarbingDurationMinutes = *input.BarbingDurationMinutes
	}
	if input.CapacityPerSlot != nil {
		if err := validateShopTiming(shop.BarbingDurationMinutes); err != nil {
			return nil, models.Shop{}, err
		}
		shop.CapacityPerSlot = *input.CapacityPerSlot
	}

	days := make([]models.BusinessDay, 0, 7)
	for weekday := 0; weekday < 7; weekday++ {
		_, active := activeSet[weekday]
		days = append(days, models.BusinessDay{
			ShopID:    shop.ID,
			Weekday:   weekday,
			IsActive:  active,
			OpenTime:  openTime,
			CloseTime: closeTime,
		})
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&shop).Updates(map[string]interface{}{
			"barbing_duration_minutes": shop.BarbingDurationMinutes,
			"capacity_per_slot":        shop.CapacityPerSlot,
		}).Error; err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Update shop", "error updating shop")
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "shop_id"}, {Name: "weekday"}},
			DoUpdates: clause.AssignmentColumns([]string{"is_active", "open_time", "close_time", "updated_at"}),
		}).Create(&days).Error; err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Create business days", "error creating business days")
		}
		return refreshFutureSlots(tx, shop.ID, shop.CapacityPerSlot, true)
	})
	if err != nil {
		return nil, models.Shop{}, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Upsert business days", "error upserting business days")
	}

	return days, shop, nil
}

func (s *Service) ListBusinessDays(ctx context.Context, ownerID, shopID uuid.UUID) ([]models.BusinessDay, error) {
	if _, err := s.findOwnedShop(ctx, ownerID, shopID); err != nil {
		return nil, err
	}
	var days []models.BusinessDay
	err := s.db.WithContext(ctx).Where("shop_id = ?", shopID).Order("weekday asc").Find(&days).Error
	return days, err
}

func (s *Service) PatchBusinessDay(ctx context.Context, input PatchBusinessDayInput) (models.BusinessDay, error) {
	if _, err := s.findOwnedShop(ctx, input.OwnerID, input.ShopID); err != nil {
		return models.BusinessDay{}, err
	}

	var day models.BusinessDay
	if err := s.db.WithContext(ctx).Where("id = ? AND shop_id = ?", input.DayID, input.ShopID).First(&day).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.BusinessDay{}, errorMap.New(errorMap.CodeNotFound, "Shop Service: Find business day", ErrBusinessDayAbsent.Error())
		}
		return models.BusinessDay{}, err
	}

	updates := map[string]interface{}{}
	if input.IsActive != nil {
		updates["is_active"] = *input.IsActive
	}
	if input.OpenTime != nil || input.CloseTime != nil {
		open := day.OpenTime
		close := day.CloseTime
		if input.OpenTime != nil {
			open = *input.OpenTime
		}
		if input.CloseTime != nil {
			close = *input.CloseTime
		}
		normalizedOpen, normalizedClose, err := normalizeSchedule(open, close)
		if err != nil {
			return models.BusinessDay{}, err
		}
		updates["open_time"] = normalizedOpen
		updates["close_time"] = normalizedClose
	}

	if len(updates) > 0 {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&day).Updates(updates).Error; err != nil {
				return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Update business days", "unable to update business day")
			}
			return refreshFutureSlots(tx, input.ShopID, 0, true)
		})
		if err != nil {
			return models.BusinessDay{}, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Line 323", "unable to complete transaction")
		}
	}
	return day, nil
}

func (s *Service) AddBlockedDate(ctx context.Context, input BlockedDateInput) (models.BlockedDate, error) {
	shop, err := s.findOwnedShop(ctx, input.OwnerID, input.ShopID)
	if err != nil {
		return models.BlockedDate{}, err
	}
	loc, _ := time.LoadLocation(shop.Timezone)
	date, err := utils.ParseDateInLocation(input.Date, loc)
	if err != nil {
		return models.BlockedDate{}, err
	}

	blocked := models.BlockedDate{
		ShopID: shop.ID,
		Date:   date,
		Reason: strings.TrimSpace(input.Reason),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "shop_id"}, {Name: "date"}},
			DoUpdates: clause.AssignmentColumns([]string{"reason", "updated_at"}),
		}).Create(&blocked).Error; err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Create blocked date", "error creating blocked date")
		}
		return nil
	})
	if err != nil {
		return models.BlockedDate{}, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Add blocked date", "error adding blocked date")
	}
	return blocked, nil
}

func (s *Service) ListBlockedDates(ctx context.Context, ownerID, shopID uuid.UUID) ([]models.BlockedDate, error) {
	if _, err := s.findOwnedShop(ctx, ownerID, shopID); err != nil {
		return nil, err
	}
	var dates []models.BlockedDate
	err := s.db.WithContext(ctx).Where("shop_id = ?", shopID).Order("date asc").Find(&dates).Error
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: List blocked dates", "error listing blocked dates")
	}
	return dates, nil
}

func (s *Service) DeleteBlockedDate(ctx context.Context, ownerID, shopID, blockedID uuid.UUID) error {
	_, err := s.findOwnedShop(ctx, ownerID, shopID)
	if err != nil {
		return err
	}
	var blocked models.BlockedDate
	if err := s.db.WithContext(ctx).Where("id = ? AND shop_id = ?", blockedID, shopID).First(&blocked).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errorMap.New(errorMap.CodeNotFound, "Shop Service: Find blocked date", "blocked date not found")
		}
		return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Find blocked date", "blocked date not found")
	}

	if err := s.db.Delete(&blocked).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Delete blocked date", "failed to delete blocked date")
	}
	return nil
}

func (s *Service) AddService(ctx context.Context, payload AddServiceInput) (*models.Shop, error) {
	shop, err := s.findOwnedShop(ctx, payload.OwnerID, payload.ShopID)
	if err != nil {
		return nil, err
	}

	if err := validateShopTiming(payload.BarbingDurationMinutes); err != nil {
		return nil, err
	}
	service := models.Service{
		ShopID:            payload.ShopID,
		Name:              payload.Name,
		Description:       payload.Description,
		Price:             payload.Price,
		DurationInMinutes: payload.BarbingDurationMinutes,
		IsActive:          true,
	}

	if err := s.db.Create(&service).Error; err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Add service", "error adding service")
	}
	return &shop, nil
}

func (s *Service) ListServices(ctx context.Context, shopID uuid.UUID) ([]models.Service, error) {
	var services []models.Service
	err := s.db.WithContext(ctx).Where("shop_id = ?", shopID).Find(&services).Error
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: List services", "error listing services")
	}
	return services, nil
}

func (s *Service) GetService(ctx context.Context, serviceID uuid.UUID) (models.Service, error) {
	var service models.Service
	err := s.db.WithContext(ctx).Where("id = ?", serviceID).First(&service).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Service{}, errorMap.New(errorMap.CodeNotFound, "Shop Service: Get service", "service not found")
	}
	if err != nil {
		return models.Service{}, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Get service", "error retrieving service")
	}
	return service, nil
}

func (s *Service) DeleteService(ctx context.Context, ownerID, serviceID uuid.UUID) error {
	var service models.Service
	err := s.db.WithContext(ctx).Where("id = ?", serviceID).First(&service).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errorMap.New(errorMap.CodeNotFound, "Shop Service: Find service", "service not found")
	}
	if err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Find service", "error retrieving service")
	}

	if _, err := s.findOwnedShop(ctx, ownerID, service.ShopID); err != nil {
		return err
	}

	if err := s.db.Delete(&service).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Delete service", "error deleting service")
	}
	return nil
}

func (s *Service) UpdateService(ctx context.Context, payload UpdateServiceInput) (*models.Service, error) {
	_, err := s.findOwnedShop(ctx, payload.OwnerID, payload.ShopID)
	if err != nil {
		return nil, err
	}

	var service models.Service
	err = s.db.WithContext(ctx).Where("id = ? AND shop_id = ?", payload.ServiceID, payload.ShopID).First(&service).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errorMap.New(errorMap.CodeNotFound, "Shop Service: Find service", "service not found")
	}
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Find service", "error retrieving service")
	}

	updated := false

	if payload.Name != "" {
		service.Name = payload.Name
		updated = true
	}
	if payload.Description != "" {
		service.Description = payload.Description
		updated = true
	}
	if payload.Price != 0 && payload.Price < 0 {
		service.Price = payload.Price
		updated = true
	}
	if payload.BarbingDurationMinutes != 0 {
		if err := validateShopTiming(payload.BarbingDurationMinutes); err != nil {
			return nil, err
		}
		service.DurationInMinutes = payload.BarbingDurationMinutes
		updated = true
	}

	if !updated {
		return &service, nil
	}

	if err := s.db.WithContext(ctx).Save(&service).Error; err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Shop Service: Update service", "error updating service")
	}
	return &service, nil
}
func (s *Service) findOwnedShop(ctx context.Context, ownerID, shopID uuid.UUID) (models.Shop, error) {
	var shop models.Shop
	err := s.db.WithContext(ctx).Where("id = ?", shopID).First(&shop).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Shop{}, errorMap.New(errorMap.CodeNotFound, "Shop Service: Find Owned Shop", ErrShopNotFound.Error())
	}
	if err != nil {
		return models.Shop{}, errorMap.New(errorMap.CodeInternal, "Shop Service: Find Owned Shop", "failed to find shop")
	}
	if shop.OwnerID != ownerID {
		return models.Shop{}, errorMap.New(errorMap.CodeForbidden, "Shop Service: Find Owned Shop", ErrForbiddenShop.Error())
	}
	return shop, nil
}

func (s *Service) uniqueSlug(ctx context.Context, base string) string {
	return s.uniqueSlugExcluding(ctx, base, uuid.Nil)
}

func (s *Service) uniqueSlugExcluding(ctx context.Context, base string, exclude uuid.UUID) string {
	slug := base
	for i := 0; i < 20; i++ {
		var count int64
		query := s.db.WithContext(ctx).Model(&models.Shop{}).Where("slug = ?", slug)
		if exclude != uuid.Nil {
			query = query.Where("id <> ?", exclude)
		}
		if err := query.Count(&count).Error; err != nil || count == 0 {
			return slug
		}
		suffix, err := utils.RandomString(4, "abcdefghijklmnopqrstuvwxyz0123456789")
		if err != nil {
			return fmt.Sprintf("%s-%d", base, time.Now().Unix())
		}
		slug = fmt.Sprintf("%s-%s", base, suffix)
	}
	return fmt.Sprintf("%s-%d", base, time.Now().Unix())
}

func validateTimezone(value string) error {
	if strings.TrimSpace(value) == "" {
		return errorMap.New(errorMap.CodeInvalidInput, "Validate Timezone", "timezone is required")
	}
	_, err := time.LoadLocation(value)
	if err != nil {
		return errorMap.New(errorMap.CodeInvalidInput, "Validate Timezone", "invalid timezone")
	}
	return nil
}

func validateShopTiming(durationMinutes int) error {
	if durationMinutes <= 0 || durationMinutes > 120 {
		return errorMap.New(errorMap.CodeInvalidInput, "Validate Shop Timing", "barbing_duration must be between 1 and 120 minutes")
	}
	return nil
}

func normalizeSchedule(open, close string) (string, string, error) {
	openTime, err := utils.NormalizeClock(open)
	if err != nil {
		return "", "", err
	}
	closeTime, err := utils.NormalizeClock(close)
	if err != nil {
		return "", "", err
	}
	openParsed, _ := time.Parse("15:04", openTime)
	closeParsed, _ := time.Parse("15:04", closeTime)
	if !closeParsed.After(openParsed) {
		return "", "", errorMap.New(errorMap.CodeInvalidInput, "Validate Schedule", "close time cannot come before open time")
	}
	return openTime, closeTime, nil
}

func activeDaySet(allDays bool, activeDays []int) (map[int]struct{}, error) {
	out := map[int]struct{}{}
	if allDays {
		for i := 0; i < 7; i++ {
			out[i] = struct{}{}
		}
		return out, nil
	}
	for _, day := range activeDays {
		if day < 0 || day > 6 {
			return nil, errorMap.New(errorMap.CodeInvalidInput, "Validate Active Days", "active_days values must be between 0 and 6")
		}
		out[day] = struct{}{}
	}
	if len(out) == 0 {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "Validate Active Days", "active_days is required unless all_days is true")
	}
	return out, nil
}

func refreshFutureSlots(tx *gorm.DB, shopID uuid.UUID, capacity int, resetLayout bool) error {
	now := time.Now().UTC()
	if resetLayout {
		if err := tx.Where("shop_id = ? AND starts_at > ? AND booked_count = 0", shopID, now).Delete(&models.Slot{}).Error; err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Reset Slots", "Error resetting slots")
		}
	}
	if capacity > 0 {
		return tx.Model(&models.Slot{}).
			Where("shop_id = ? AND starts_at > ?", shopID, now).
			Update("capacity", gorm.Expr("GREATEST(?, booked_count)", capacity)).Error
	}
	return nil
}

func dayRange(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return start, start.AddDate(0, 0, 1)
}
