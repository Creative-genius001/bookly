package slots

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

var (
	ErrShopUnavailable = errors.New("shop is not available for booking")
	ErrSlotWindow      = errors.New("slot date is outside the booking window")
)

type Service struct {
	db     *gorm.DB
	logger *slog.Logger
}

func NewService(db *gorm.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

func (s *Service) AvailabilityForDate(
	ctx context.Context,
	shopSlug string,
	dateValue string,
) ([]models.AvailableSlot, error) {

	var shop models.Shop

	if err := s.db.WithContext(ctx).
		Where("slug = ?", utils.Slugify(shopSlug)).
		First(&shop).Error; err != nil {

		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorMap.New(
				errorMap.CodeNotFound,
				"Slot Service",
				"shop not found",
			)
		}

		return nil, err
	}

	loc, err := time.LoadLocation(shop.Timezone)
	if err != nil {
		return nil, err
	}

	date, err := utils.ParseDateInLocation(dateValue, loc)
	if err != nil {
		return nil, err
	}

	if err := utils.ValidateBookingWindow(
		date,
		loc,
	); err != nil {
		return nil, err
	}

	blocked, err := s.isBlocked(
		ctx,
		shop.ID.String(),
		date,
	)

	if err != nil {
		return nil, err
	}

	if blocked {
		return []models.AvailableSlot{}, nil
	}

	var day models.BusinessDay

	err = s.db.WithContext(ctx).
		Where(
			"shop_id = ? AND weekday = ? AND is_active = true",
			shop.ID,
			int(date.Weekday()),
		).
		First(&day).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []models.AvailableSlot{}, nil
	}

	if err != nil {
		return nil, err
	}

	return s.generateAvailability(
		ctx,
		shop,
		date,
		day,
		loc,
	)
}

func (s *Service) generateAvailability(
	ctx context.Context,
	shop models.Shop,
	date time.Time,
	day models.BusinessDay,
	loc *time.Location,
) ([]models.AvailableSlot, error) {

	openAt, err := utils.ClockOnDate(
		date,
		day.OpenTime,
		loc,
	)

	if err != nil {
		return nil, err
	}

	closeAt, err := utils.ClockOnDate(
		date,
		day.CloseTime,
		loc,
	)

	if err != nil {
		return nil, err
	}

	duration := time.Duration(
		shop.BarbingDurationMinutes,
	) * time.Minute

	slotInterval := 15 * time.Minute

	var bookings []models.Booking

	if err := s.db.WithContext(ctx).
		Where(
			"shop_id = ? AND status IN ?",
			shop.ID,
			[]string{
				"pending_payment",
				"confirmed",
			},
		).
		Where(
			"starts_at >= ? AND starts_at < ?",
			openAt.UTC(),
			closeAt.UTC(),
		).
		Find(&bookings).
		Error; err != nil {

		return nil, err
	}

	var available []models.AvailableSlot

	for start := openAt; ; start = start.Add(slotInterval) {

		end := start.Add(duration)

		if end.After(closeAt) {
			break
		}

		overlapCount := countOverlaps(
			start.UTC(),
			end.UTC(),
			bookings,
		)

		if overlapCount < shop.CapacityPerSlot && start.After(time.Now()) {

			available = append(
				available,
				models.AvailableSlot{
					Start: start,
					End:   end,
				},
			)
		}
	}

	return available, nil
}

func countOverlaps(
	start time.Time,
	end time.Time,
	bookings []models.Booking,
) int {

	count := 0

	for _, booking := range bookings {

		if overlaps(
			start,
			end,
			booking.StartsAt,
			booking.EndsAt,
		) {
			count++
		}
	}

	return count
}

func overlaps(
	startA time.Time,
	endA time.Time,
	startB time.Time,
	endB time.Time,
) bool {

	return startA.Before(endB) &&
		endA.After(startB)
}

func (s *Service) isBlocked(
	ctx context.Context,
	shopID string,
	date time.Time,
) (bool, error) {

	var count int64

	err := s.db.WithContext(ctx).
		Model(&models.BlockedDate{}).
		Where(
			"shop_id = ? AND date = ?",
			shopID,
			date,
		).
		Count(&count).
		Error
	if err != nil {
		errorMap.Wrap(err, errorMap.CodeInternal, "Slot Service: Is Blocked", "Error getting blocked date")
		return false, err
	}

	return count > 0, nil
}
