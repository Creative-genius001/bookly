package slots

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
)

var (
	ErrShopUnavailable = errors.New("shop is not available for booking")
	ErrSlotWindow      = errors.New("slot date is outside the booking window")
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) SlotsForDate(ctx context.Context, shopSlug, dateValue string, now time.Time) ([]models.Slot, error) {
	var shop models.Shop
	if err := s.db.WithContext(ctx).Where("slug = ?", utils.Slugify(shopSlug)).First(&shop).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrShopUnavailable
		}
		return nil, err
	}
	if !shop.IsActive {
		return nil, ErrShopUnavailable
	}
	if shop.BarbingDurationMinutes <= 0 || shop.CapacityPerSlot <= 0 {
		return nil, fmt.Errorf("shop slot configuration is incomplete")
	}

	loc, err := time.LoadLocation(shop.Timezone)
	if err != nil {
		return nil, err
	}
	date, err := utils.ParseDateInLocation(dateValue, loc)
	if err != nil {
		return nil, err
	}
	if err := utils.ValidateBookingWindow(date, loc, now); err != nil {
		return nil, ErrSlotWindow
	}

	if blocked, err := s.isBlocked(ctx, shop.ID, date); err != nil || blocked {
		return []models.Slot{}, err
	}

	var day models.BusinessDay
	err = s.db.WithContext(ctx).
		Where("shop_id = ? AND weekday = ? AND is_active = true", shop.ID, int(date.Weekday())).
		First(&day).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []models.Slot{}, nil
	}
	if err != nil {
		return nil, err
	}

	starts, err := BuildSlotTimes(date, day.OpenTime, day.CloseTime, shop.BarbingDurationMinutes, loc)
	if err != nil {
		return nil, err
	}
	if len(starts) == 0 {
		return []models.Slot{}, nil
	}

	toCreate := make([]models.Slot, 0, len(starts))
	for _, start := range starts {
		toCreate = append(toCreate, models.Slot{
			ShopID:   shop.ID,
			StartsAt: start.UTC(),
			EndsAt:   start.Add(time.Duration(shop.BarbingDurationMinutes) * time.Minute).UTC(),
			Capacity: shop.CapacityPerSlot,
			Status:   models.SlotAvailable,
		})
	}

	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shop_id"}, {Name: "starts_at"}},
		DoNothing: true,
	}).Create(&toCreate).Error; err != nil {
		return nil, err
	}

	dayStart := starts[0].UTC()
	dayEnd := starts[len(starts)-1].Add(time.Duration(shop.BarbingDurationMinutes) * time.Minute).UTC()
	var persisted []models.Slot
	if err := s.db.WithContext(ctx).
		Where("shop_id = ? AND starts_at >= ? AND starts_at < ?", shop.ID, dayStart, dayEnd).
		Order("starts_at asc").
		Find(&persisted).Error; err != nil {
		return nil, err
	}

	available := make([]models.Slot, 0, len(persisted))
	nowUTC := now.UTC()
	for _, slot := range persisted {
		slot.Status = computedStatus(slot, nowUTC)
		if slot.Status == models.SlotExpired || slot.Status == models.SlotBlocked {
			continue
		}
		available = append(available, slot)
	}

	return available, nil
}

func BuildSlotTimes(date time.Time, openClock, closeClock string, durationMinutes int, loc *time.Location) ([]time.Time, error) {
	if durationMinutes <= 0 {
		return nil, fmt.Errorf("duration must be positive")
	}
	openAt, err := utils.ClockOnDate(date, openClock, loc)
	if err != nil {
		return nil, err
	}
	closeAt, err := utils.ClockOnDate(date, closeClock, loc)
	if err != nil {
		return nil, err
	}
	if !closeAt.After(openAt) {
		return nil, fmt.Errorf("close_time must be after open_time")
	}

	duration := time.Duration(durationMinutes) * time.Minute
	var starts []time.Time
	for start := openAt; !start.Add(duration).After(closeAt); start = start.Add(duration) {
		starts = append(starts, start)
	}
	return starts, nil
}

func (s *Service) isBlocked(ctx context.Context, shopID interface{}, date time.Time) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.BlockedDate{}).
		Where("shop_id = ? AND date = ?", shopID, date).
		Count(&count).Error
	return count > 0, err
}

func computedStatus(slot models.Slot, now time.Time) models.SlotStatus {
	if slot.StartsAt.Before(now) || slot.StartsAt.Equal(now) {
		return models.SlotExpired
	}
	if slot.Status == models.SlotBlocked {
		return models.SlotBlocked
	}
	if slot.BookedCount >= slot.Capacity {
		return models.SlotFull
	}
	return models.SlotAvailable
}
