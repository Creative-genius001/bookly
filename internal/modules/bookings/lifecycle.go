package bookings

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"barber-booking-backend/internal/models"
	errorMap "barber-booking-backend/internal/utils/error"
)

// cancelLeadTime is the minimum notice required to cancel or reschedule.
const cancelLeadTime = time.Hour

// Cancel cancels a booking (public, by code). Confirmed (paid) bookings are
// refunded via Paystack. Cancelling frees the slot automatically because
// availability is computed from confirmed/pending bookings only.
func (s *Service) Cancel(ctx context.Context, code string) (BookingListItem, error) {
	booking, err := s.loadBookingByCode(ctx, code)
	if err != nil {
		return BookingListItem{}, err
	}

	if booking.Status != models.BookingConfirmed && booking.Status != models.BookingPendingPayment {
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Cancel", "this booking can no longer be cancelled")
	}
	if !booking.StartsAt.After(time.Now().UTC().Add(cancelLeadTime)) {
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Cancel", ErrCancelNotAllowed.Error())
	}

	var refundRef string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Pessimistically lock the shop's wallet row before the refund debit so
		// it serializes against concurrent withdrawals (same lock ordering as
		// the payouts service — shop row first — to avoid deadlocks).
		var lockRow models.Shop
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").First(&lockRow, "id = ?", booking.ShopID).Error; err != nil {
			return err
		}

		bookingUpdate := (map[string]any{
			"status":       models.BookingCancelled,
			"cancelled_at": time.Now().UTC(),
		})
		if err := tx.Model(&models.Booking{}).Where("id = ?", booking.ID).
			Updates(bookingUpdate).Error; err != nil {
			return err
		}
		// Refund a successful payment, if any.
		var payment models.Payment
		err := tx.Where("booking_id = ? AND status = ?", booking.ID, models.PaymentSuccess).
			First(&payment).Error
		if err == nil {
			refundRef = payment.Reference
			if err := tx.Model(&payment).Update("status", models.PaymentRefunded).Error; err != nil {
				return err
			}
			// Reverse the shop's wallet credit (idempotent on reference+kind).
			net := payment.AmountKobo - (payment.AmountKobo*int64(s.platformFeePercent))/100
			debit := models.WalletEntry{
				ShopID:     booking.ShopID,
				Type:       models.WalletDebit,
				AmountKobo: net,
				Reason:     "Refund " + booking.Code,
				BookingID:  &booking.ID,
				Reference:  payment.Reference,
				EntryKind:  "booking_refund",
			}
			if err := tx.Create(&debit).Error; err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return BookingListItem{}, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Cancel", "could not cancel booking")
	}

	// Side effects outside the transaction.
	if refundRef != "" {
		if err := s.paystack.Refund(ctx, refundRef); err != nil {
			s.logger.ErrorContext(ctx, "paystack refund failed", "reference", refundRef, "error", err)
		}
	}
	booking.Status = models.BookingCancelled
	_ = s.notifier.BookingCancelled(ctx, booking)

	return s.GetByCode(ctx, code)
}

// Reschedule moves a confirmed booking to a new start time (public, by code).
func (s *Service) Reschedule(ctx context.Context, code string, newStart time.Time) (BookingListItem, error) {
	booking, err := s.loadBookingByCode(ctx, code)
	if err != nil {
		return BookingListItem{}, err
	}

	if booking.Status != models.BookingConfirmed {
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Reschedule", "only confirmed bookings can be rescheduled")
	}
	if !booking.StartsAt.After(time.Now().UTC().Add(cancelLeadTime)) {
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Reschedule", ErrRescheduleNotAllowed.Error())
	}

	service, err := s.findService(ctx, booking.ServiceID)
	if err != nil {
		return BookingListItem{}, err
	}
	newStart = newStart.UTC()
	newEnd := newStart.Add(time.Duration(service.DurationInMinutes) * time.Minute)

	if newStart.Equal(booking.StartsAt) {
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Reschedule", "choose a different time")
	}
	if err := validateBookingTime(newStart, newEnd, time.Now().UTC()); err != nil {
		return BookingListItem{}, err
	}

	shop, err := s.findShop(ctx, booking.ShopID)
	if err != nil {
		return BookingListItem{}, err
	}

	// Lock the target slot for the duration of the availability check + move.
	key := setLockKey(booking.ServiceID.String(), newStart)
	lock, err := s.locker.Acquire(ctx, key, 15*time.Second)
	if err != nil {
		return BookingListItem{}, errorMap.New(errorMap.CodeInternal, "Booking Service: Reschedule", ErrSlotNotBookable.Error())
	}
	defer func() { _ = s.locker.Release(ctx, lock) }()

	ok, err := s.slotHasCapacity(ctx, shop.CapacityPerSlot, booking.ShopID, newStart, newEnd, booking.ID)
	if err != nil {
		return BookingListItem{}, err
	}
	if !ok {
		return BookingListItem{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Reschedule", ErrSlotCapacityExceeded.Error())
	}

	if err := s.db.WithContext(ctx).Model(&models.Booking{}).Where("id = ?", booking.ID).
		Updates(map[string]interface{}{"starts_at": newStart, "ends_at": newEnd}).Error; err != nil {
		return BookingListItem{}, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Reschedule", "could not reschedule booking")
	}

	booking.StartsAt = newStart
	booking.EndsAt = newEnd
	_ = s.notifier.BookingRescheduled(ctx, booking)

	return s.GetByCode(ctx, code)
}

func (s *Service) loadBookingByCode(ctx context.Context, code string) (models.Booking, error) {
	var booking models.Booking
	err := s.db.WithContext(ctx).Where("code = ?", code).First(&booking).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Booking{}, errorMap.New(errorMap.CodeNotFound, "Booking Service", ErrBookingNotFound.Error())
	}
	if err != nil {
		return models.Booking{}, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service", "could not load booking")
	}
	return booking, nil
}

// slotHasCapacity is isSlotAvailable but able to exclude one booking (the one
// being rescheduled) from the overlap count.
func (s *Service) slotHasCapacity(ctx context.Context, capacity int, shopID uuid.UUID, start, end time.Time, excludeBookingID uuid.UUID) (bool, error) {
	statuses := []string{string(models.BookingConfirmed), string(models.BookingPendingPayment)}
	q := s.db.WithContext(ctx).Model(&models.Booking{}).
		Where("shop_id = ? AND status IN ? AND (starts_at < ? AND ends_at > ?)", shopID, statuses, end, start)
	if excludeBookingID != uuid.Nil {
		q = q.Where("id <> ?", excludeBookingID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Reschedule", "could not check availability")
	}
	return count < int64(capacity), nil
}
