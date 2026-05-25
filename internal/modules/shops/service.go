package shops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
)

var (
	ErrShopNotFound      = errors.New("shop not found")
	ErrOwnerHasShop      = errors.New("owner can only have one shop")
	ErrForbiddenShop     = errors.New("shop does not belong to owner")
	ErrInvalidSchedule   = errors.New("invalid schedule")
	ErrBusinessDayAbsent = errors.New("business day not found")
)

type Service struct {
	db *gorm.DB
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

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
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
	if err := validateShopTiming(input.BarbingDurationMinutes, input.CapacityPerSlot); err != nil {
		return models.Shop{}, err
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Shop{}).Where("owner_id = ?", input.OwnerID).Count(&count).Error; err != nil {
		return models.Shop{}, err
	}
	if count > 0 {
		return models.Shop{}, ErrOwnerHasShop
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
		return models.Shop{}, ErrShopNotFound
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
		if err := validateShopTiming(*input.BarbingDurationMinutes, shop.CapacityPerSlot); err != nil {
			return models.Shop{}, err
		}
		updates["barbing_duration_minutes"] = *input.BarbingDurationMinutes
	}
	if input.CapacityPerSlot != nil {
		if err := validateShopTiming(shop.BarbingDurationMinutes, *input.CapacityPerSlot); err != nil {
			return models.Shop{}, err
		}
		updates["capacity_per_slot"] = *input.CapacityPerSlot
	}

	if len(updates) > 0 {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&shop).Updates(updates).Error; err != nil {
				return err
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
		if err := validateShopTiming(*input.BarbingDurationMinutes, shop.CapacityPerSlot); err != nil {
			return nil, models.Shop{}, err
		}
		shop.BarbingDurationMinutes = *input.BarbingDurationMinutes
	}
	if input.CapacityPerSlot != nil {
		if err := validateShopTiming(shop.BarbingDurationMinutes, *input.CapacityPerSlot); err != nil {
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
			return err
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "shop_id"}, {Name: "weekday"}},
			DoUpdates: clause.AssignmentColumns([]string{"is_active", "open_time", "close_time", "updated_at"}),
		}).Create(&days).Error; err != nil {
			return err
		}
		return refreshFutureSlots(tx, shop.ID, shop.CapacityPerSlot, true)
	})
	if err != nil {
		return nil, models.Shop{}, err
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
			return models.BusinessDay{}, ErrBusinessDayAbsent
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
				return err
			}
			return refreshFutureSlots(tx, input.ShopID, 0, true)
		})
		if err != nil {
			return models.BusinessDay{}, err
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
			return err
		}
		start, end := dayRange(date)
		return tx.Model(&models.Slot{}).
			Where("shop_id = ? AND starts_at >= ? AND starts_at < ? AND starts_at > ? AND booked_count = 0", shop.ID, start.UTC(), end.UTC(), time.Now().UTC()).
			Update("status", models.SlotBlocked).Error
	})
	return blocked, err
}

func (s *Service) ListBlockedDates(ctx context.Context, ownerID, shopID uuid.UUID) ([]models.BlockedDate, error) {
	if _, err := s.findOwnedShop(ctx, ownerID, shopID); err != nil {
		return nil, err
	}
	var dates []models.BlockedDate
	err := s.db.WithContext(ctx).Where("shop_id = ?", shopID).Order("date asc").Find(&dates).Error
	return dates, err
}

func (s *Service) DeleteBlockedDate(ctx context.Context, ownerID, shopID, blockedID uuid.UUID) error {
	shop, err := s.findOwnedShop(ctx, ownerID, shopID)
	if err != nil {
		return err
	}
	var blocked models.BlockedDate
	if err := s.db.WithContext(ctx).Where("id = ? AND shop_id = ?", blockedID, shopID).First(&blocked).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrShopNotFound
		}
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&blocked).Error; err != nil {
			return err
		}
		loc, _ := time.LoadLocation(shop.Timezone)
		localDate := time.Date(blocked.Date.Year(), blocked.Date.Month(), blocked.Date.Day(), 0, 0, 0, 0, loc)
		start, end := dayRange(localDate)
		return tx.Model(&models.Slot{}).
			Where("shop_id = ? AND starts_at >= ? AND starts_at < ? AND starts_at > ? AND booked_count = 0 AND status = ?", shopID, start.UTC(), end.UTC(), time.Now().UTC(), models.SlotBlocked).
			Update("status", models.SlotAvailable).Error
	})
}

func (s *Service) findOwnedShop(ctx context.Context, ownerID, shopID uuid.UUID) (models.Shop, error) {
	var shop models.Shop
	err := s.db.WithContext(ctx).Where("id = ?", shopID).First(&shop).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Shop{}, ErrShopNotFound
	}
	if err != nil {
		return models.Shop{}, err
	}
	if shop.OwnerID != ownerID {
		return models.Shop{}, ErrForbiddenShop
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
		return fmt.Errorf("timezone is required")
	}
	_, err := time.LoadLocation(value)
	if err != nil {
		return fmt.Errorf("invalid timezone")
	}
	return nil
}

func validateShopTiming(durationMinutes, capacity int) error {
	if durationMinutes <= 0 || durationMinutes > 480 {
		return fmt.Errorf("barbing_duration must be between 1 and 480 minutes")
	}
	if capacity <= 0 {
		return fmt.Errorf("capacity_per_slot must be at least 1")
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
		return "", "", ErrInvalidSchedule
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
			return nil, fmt.Errorf("active_days values must be between 0 and 6")
		}
		out[day] = struct{}{}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("active_days is required unless all_days is true")
	}
	return out, nil
}

func refreshFutureSlots(tx *gorm.DB, shopID uuid.UUID, capacity int, resetLayout bool) error {
	now := time.Now().UTC()
	if resetLayout {
		if err := tx.Where("shop_id = ? AND starts_at > ? AND booked_count = 0", shopID, now).Delete(&models.Slot{}).Error; err != nil {
			return err
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
