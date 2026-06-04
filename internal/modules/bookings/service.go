package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"barber-booking-backend/internal/locks"
	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/notifications"
	"barber-booking-backend/internal/paystack"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

var (
	ErrBookingNotFound      = errors.New("booking not found")
	ErrSlotNotBookable      = errors.New("slot is not bookable")
	ErrBookingWindow        = errors.New("slot is outside the rolling 14-day booking window")
	ErrRescheduleNotAllowed = errors.New("reschedule is not allowed")
	ErrCancelNotAllowed     = errors.New("booking cannot be cancelled within 1 hour of appointment")
	ErrPaymentNotFound      = errors.New("payment not found")
)

type Service struct {
	db         *gorm.DB
	locker     *locks.RedisLocker
	paystack   *paystack.Client
	notifier   *notifications.Notifier
	amountKobo int64
}

type InitiateResult struct {
	Booking          models.Booking `json:"booking"`
	Payment          models.Payment `json:"payment"`
	AuthorizationURL string         `json:"authorization_url"`
	AccessCode       string         `json:"access_code"`
}

func NewService(db *gorm.DB, locker *locks.RedisLocker, paystackClient *paystack.Client, notifier *notifications.Notifier, amountKobo int64) *Service {
	return &Service{
		db:         db,
		locker:     locker,
		paystack:   paystackClient,
		notifier:   notifier,
		amountKobo: amountKobo,
	}
}

func (s *Service) findService(ctx context.Context, serviceID uuid.UUID) (*models.Service, error) {
	var service models.Service
	if err := s.db.WithContext(ctx).Where("id = ?", serviceID).First(&service).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorMap.New(errorMap.CodeNotFound, "Booking Service", "service not found")
		}
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Find Service", "service not found")
	}
	return &service, nil
}

func (s *Service) Initiate(ctx context.Context, payload initiateRequest) (InitiateResult, error) {
	_, err := s.findService(ctx, payload.ServiceID)
	if err != nil {
		return InitiateResult{}, err
	}
	lock, err := s.locker.Acquire(ctx, "lock:slot:"+payload.ServiceID.String(), 15*time.Second)
	if err != nil {
		return InitiateResult{}, errorMap.New(errorMap.CodeInternal, "Booking Service: Acquire slot lock", ErrSlotNotBookable.Error())
	}
	defer func() { _ = s.locker.Release(ctx, lock) }()

	slot, shop, err := s.bookableSlot(ctx, payload.ServiceID, time.Now())
	if err != nil {
		return InitiateResult{}, err
	}

	code, err := s.uniqueBookingCode(ctx)
	if err != nil {
		return InitiateResult{}, err
	}
	reference, err := paymentReference(code)
	if err != nil {
		return InitiateResult{}, err
	}

	booking := models.Booking{
		Code:             code,
		ShopID:           shop.ID,
		ServiceID:        payload.ServiceID,
		CustomerEmail:    payload.CustomerEmail,
		CustomerName:     payload.CustomerName,
		Status:           models.BookingPendingPayment,
		StartsAt:         slot.StartsAt,
		EndsAt:           slot.EndsAt,
		PaymentReference: reference,
	}
	payment := models.Payment{
		Reference:  reference,
		AmountKobo: s.amountKobo,
		Status:     models.PaymentPending,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&booking).Error; err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Create Booking", "unable to create booking row")
		}
		payment.BookingID = booking.ID
		return tx.Create(&payment).Error
	}); err != nil {
		return InitiateResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Create Payment Transaction", "unable to create payment row")
	}

	init, err := s.paystack.InitializeTransaction(ctx, paystack.InitializeRequest{
		Email:     payload.CustomerEmail,
		Amount:    s.amountKobo,
		Reference: reference,
		Metadata: map[string]any{
			"booking_code": booking.Code,
			"booking_id":   booking.ID.String(),
			"shop_id":      shop.ID.String(),
			"service_id":   payload.ServiceID.String(),
		},
	})
	if err != nil {
		return InitiateResult{}, err
	}

	return InitiateResult{
		Booking:          booking,
		Payment:          payment,
		AuthorizationURL: init.AuthorizationURL,
		AccessCode:       init.AccessCode,
	}, nil
}

func (s *Service) InitializePayment(ctx context.Context, customerID uuid.UUID, bookingCode string) (InitiateResult, error) {
	var booking models.Booking
	if err := s.db.WithContext(ctx).
		Where("code = ? AND customer_id = ? AND status = ?", bookingCode, customerID, models.BookingPendingPayment).
		First(&booking).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InitiateResult{}, ErrBookingNotFound
		}
		return InitiateResult{}, err
	}

	var payment models.Payment
	if err := s.db.WithContext(ctx).Where("booking_id = ? AND status = ?", booking.ID, models.PaymentPending).First(&payment).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return InitiateResult{}, err
		}
		reference, err := paymentReference(booking.Code)
		if err != nil {
			return InitiateResult{}, err
		}
		payment = models.Payment{
			BookingID:  booking.ID,
			Reference:  reference,
			AmountKobo: s.amountKobo,
			Status:     models.PaymentPending,
		}
		if err := s.db.WithContext(ctx).Create(&payment).Error; err != nil {
			return InitiateResult{}, err
		}
		booking.PaymentReference = reference
		_ = s.db.WithContext(ctx).Model(&booking).Update("payment_reference", reference).Error
	}

	init, err := s.paystack.InitializeTransaction(ctx, paystack.InitializeRequest{
		Email:     booking.CustomerEmail,
		Amount:    payment.AmountKobo,
		Reference: payment.Reference,
		Metadata: map[string]any{
			"booking_code": booking.Code,
			"booking_id":   booking.ID.String(),
		},
	})
	if err != nil {
		return InitiateResult{}, err
	}

	return InitiateResult{
		Booking:          booking,
		Payment:          payment,
		AuthorizationURL: init.AuthorizationURL,
		AccessCode:       init.AccessCode,
	}, nil
}

// func (s *Service) HandlePaystackWebhook(ctx context.Context, body []byte, signature string) error {
// 	payload, err := s.paystack.ParseWebhook(body, signature)
// 	if err != nil {
// 		return err
// 	}
// 	if payload.Data.Reference == "" {
// 		return nil
// 	}

// 	var payment models.Payment
// 	if err := s.db.WithContext(ctx).Where("reference = ?", payload.Data.Reference).First(&payment).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			return nil
// 		}
// 		return errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Check payment record", "unable to check for payment record")
// 	}

// 	if payload.Event != "charge.success" || payload.Data.Status != "success" {
// 		return s.db.WithContext(ctx).Model(&payment).Updates(map[string]interface{}{
// 			"status":           models.PaymentFailed,
// 			"provider_payload": datatypes.JSON(body),
// 		}).Error
// 	}

// 	var confirmedBooking models.Booking
// 	var customer models.User
// 	needsRefund := false

// 	if err := s.db.WithContext(ctx).First(&confirmedBooking, "id = ?", payment.BookingID).Error; err != nil {
// 		return errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Find booking for payment", "unable to find booking for payment")
// 	}

// 	lock, err := s.locker.Acquire(ctx, "lock:slot:"+confirmedBooking.SlotID.String(), 15*time.Second)
// 	if err != nil {
// 		return errorMap.New(errorMap.CodeInternal, "Booking Service: Acquire slot lock", ErrSlotNotBookable.Error())
// 	}
// 	defer func() { _ = s.locker.Release(ctx, lock) }()

// 	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
// 		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", payment.ID).First(&payment).Error; err != nil {
// 			return errorMap.New(errorMap.CodeInternal, "Booking Service: Update payment", "unable to update payment")
// 		}
// 		if payment.Status == models.PaymentSuccess || payment.Status == models.PaymentRefunded {
// 			return nil
// 		}

// 		var booking models.Booking
// 		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", payment.BookingID).First(&booking).Error; err != nil {
// 			return errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Update booking", "unable to update booking")
// 		}

// 		var slot models.Slot
// 		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", booking.SlotID).First(&slot).Error; err != nil {
// 			return errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Update slot", "unable to update slot")
// 		}

// 		if booking.Status != models.BookingPendingPayment || slot.BookedCount >= slot.Capacity {
// 			needsRefund = true
// 			if err := tx.Model(&payment).Updates(map[string]interface{}{
// 				"status":           models.PaymentRefunded,
// 				"provider_payload": datatypes.JSON(body),
// 			}).Error; err != nil {
// 				return err
// 			}
// 			return tx.Model(&booking).Update("status", models.BookingExpired).Error
// 		}

// 		slot.BookedCount++
// 		slot.Status = models.SlotAvailable
// 		if slot.BookedCount >= slot.Capacity {
// 			slot.Status = models.SlotFull
// 		}

// 		if err := tx.Model(&slot).Updates(map[string]interface{}{
// 			"booked_count": slot.BookedCount,
// 			"status":       slot.Status,
// 		}).Error; err != nil {
// 			return err
// 		}
// 		if err := tx.Model(&booking).Updates(map[string]interface{}{
// 			"status":            models.BookingConfirmed,
// 			"payment_reference": payment.Reference,
// 		}).Error; err != nil {
// 			return err
// 		}
// 		if err := tx.Model(&payment).Updates(map[string]interface{}{
// 			"status":           models.PaymentSuccess,
// 			"provider_payload": datatypes.JSON(body),
// 		}).Error; err != nil {
// 			return err
// 		}

// 		confirmedBooking = booking
// 		confirmedBooking.Status = models.BookingConfirmed
// 		return tx.First(&customer, "id = ?", booking.CustomerID).Error
// 	})
// 	if err != nil {
// 		return err
// 	}

// 	if needsRefund {
// 		_ = s.paystack.Refund(ctx, payment.Reference)
// 		return nil
// 	}
// 	if customer.ID != uuid.Nil {
// 		_ = s.notifier.PaymentSuccess(ctx, customer, confirmedBooking)
// 		_ = s.notifier.BookingConfirmed(ctx, customer, confirmedBooking)
// 	}
// 	return nil
// }

// func (s *Service) Reschedule(ctx context.Context, customerID uuid.UUID, code string, newSlotID uuid.UUID) (models.Booking, error) {
// 	var booking models.Booking
// 	if err := s.db.WithContext(ctx).
// 		Where("code = ? AND customer_id = ?", code, customerID).
// 		First(&booking).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			return models.Booking{}, errorMap.New(errorMap.CodeNotFound, "Booking Service: Reschedule booking", ErrBookingNotFound.Error())
// 		}
// 		return models.Booking{}, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Reschedule booking", "unable to reschedule booking")
// 	}
// 	if booking.Status != models.BookingConfirmed || booking.RescheduledCount >= 1 || !booking.StartsAt.After(time.Now()) || booking.SlotID == newSlotID {
// 		return models.Booking{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Reschedule booking", ErrRescheduleNotAllowed.Error())
// 	}

// 	newSlot, _, err := s.bookableSlot(ctx, newSlotID, time.Now())
// 	if err != nil {
// 		return models.Booking{}, err
// 	}

// 	lock, err := s.locker.Acquire(ctx, "lock:slot:"+newSlotID.String(), 15*time.Second)
// 	if err != nil {
// 		return models.Booking{}, ErrSlotNotBookable
// 	}
// 	defer func() { _ = s.locker.Release(ctx, lock) }()

// 	var customer models.User
// 	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
// 		var oldSlot models.Slot
// 		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", booking.SlotID).First(&oldSlot).Error; err != nil {
// 			return err
// 		}
// 		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", newSlot.ID).First(&newSlot).Error; err != nil {
// 			return err
// 		}
// 		if newSlot.BookedCount >= newSlot.Capacity {
// 			return ErrSlotNotBookable
// 		}

// 		if oldSlot.BookedCount > 0 {
// 			oldSlot.BookedCount--
// 		}
// 		oldSlot.Status = models.SlotAvailable
// 		if err := tx.Model(&oldSlot).Updates(map[string]interface{}{"booked_count": oldSlot.BookedCount, "status": oldSlot.Status}).Error; err != nil {
// 			return err
// 		}

// 		newSlot.BookedCount++
// 		newSlot.Status = models.SlotAvailable
// 		if newSlot.BookedCount >= newSlot.Capacity {
// 			newSlot.Status = models.SlotFull
// 		}
// 		if err := tx.Model(&newSlot).Updates(map[string]interface{}{"booked_count": newSlot.BookedCount, "status": newSlot.Status}).Error; err != nil {
// 			return err
// 		}

// 		if err := tx.Model(&booking).Updates(map[string]interface{}{
// 			"slot_id":           newSlot.ID,
// 			"starts_at":         newSlot.StartsAt,
// 			"ends_at":           newSlot.EndsAt,
// 			"rescheduled_count": booking.RescheduledCount + 1,
// 		}).Error; err != nil {
// 			return err
// 		}
// 		if err := tx.First(&customer, "id = ?", booking.CustomerID).Error; err != nil {
// 			return err
// 		}
// 		booking.SlotID = newSlot.ID
// 		booking.StartsAt = newSlot.StartsAt
// 		booking.EndsAt = newSlot.EndsAt
// 		booking.RescheduledCount++
// 		return nil
// 	})
// 	if err != nil {
// 		return models.Booking{}, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Reschedule booking", "unable to reschedule booking")
// 	}

// 	_ = s.notifier.BookingRescheduled(ctx, customer, booking)
// 	return booking, nil
// }

// func (s *Service) Cancel(ctx context.Context, customerID uuid.UUID, code string) (models.Booking, error) {
// 	var booking models.Booking
// 	if err := s.db.WithContext(ctx).
// 		Where("code = ? AND customer_id = ?", code, customerID).
// 		First(&booking).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			return models.Booking{}, ErrBookingNotFound
// 		}
// 		return models.Booking{}, err
// 	}
// 	if booking.Status != models.BookingConfirmed && booking.Status != models.BookingPendingPayment {
// 		return models.Booking{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Cancel booking", ErrCancelNotAllowed.Error())
// 	}
// 	if !booking.StartsAt.After(time.Now().Add(time.Hour)) {
// 		return models.Booking{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Cancel booking", ErrCancelNotAllowed.Error())
// 	}

// 	var customer models.User
// 	var payment models.Payment
// 	now := time.Now().UTC()
// 	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
// 		if booking.Status == models.BookingConfirmed {
// 			var slot models.Slot
// 			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", booking.SlotID).First(&slot).Error; err != nil {
// 				return err
// 			}
// 			if slot.BookedCount > 0 {
// 				slot.BookedCount--
// 			}
// 			slot.Status = models.SlotAvailable
// 			if err := tx.Model(&slot).Updates(map[string]interface{}{"booked_count": slot.BookedCount, "status": slot.Status}).Error; err != nil {
// 				return err
// 			}
// 		}

// 		if err := tx.Model(&booking).Updates(map[string]interface{}{
// 			"status":       models.BookingCancelled,
// 			"cancelled_at": &now,
// 		}).Error; err != nil {
// 			return err
// 		}

// 		if err := tx.Where("booking_id = ? AND status = ?", booking.ID, models.PaymentSuccess).First(&payment).Error; err == nil {
// 			if err := tx.Model(&payment).Update("status", models.PaymentRefunded).Error; err != nil {
// 				return err
// 			}
// 		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
// 			return err
// 		}

// 		return tx.First(&customer, "id = ?", booking.CustomerID).Error
// 	})
// 	if err != nil {
// 		return models.Booking{}, err
// 	}

// 	if payment.Reference != "" {
// 		_ = s.paystack.Refund(ctx, payment.Reference)
// 		_ = s.notifier.RefundProcessed(ctx, customer, booking)
// 	}
// 	booking.Status = models.BookingCancelled
// 	booking.CancelledAt = &now
// 	_ = s.notifier.BookingCancelled(ctx, customer, booking)
// 	return booking, nil
// }

// func (s *Service) GetByCode(ctx context.Context, code string) (models.Booking, error) {
// 	var booking models.Booking
// 	err := s.db.WithContext(ctx).Preload("Slot").Where("code = ?", code).First(&booking).Error
// 	if errors.Is(err, gorm.ErrRecordNotFound) {
// 		return models.Booking{}, ErrBookingNotFound
// 	}
// 	return booking, err
// }

// func (s *Service) findUser(ctx context.Context, userID uuid.UUID) (models.User, error) {
// 	var user models.User
// 	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
// 		return models.User{}, errorMap.Wrap(err, errorMap.CodeInternal, "Find User", "failed to find user")
// 	}
// 	return user, nil
// }

func (s *Service) bookableSlot(ctx context.Context, slotID uuid.UUID, now time.Time) (models.Slot, models.Shop, error) {
	var slot models.Slot
	if err := s.db.WithContext(ctx).Where("id = ?", slotID).First(&slot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Slot{}, models.Shop{}, ErrSlotNotBookable
		}
		return models.Slot{}, models.Shop{}, err
	}

	var shop models.Shop
	if err := s.db.WithContext(ctx).Where("id = ?", slot.ShopID).First(&shop).Error; err != nil {
		return models.Slot{}, models.Shop{}, err
	}
	if !shop.IsActive || slot.Status == models.SlotBlocked || !slot.StartsAt.After(now.UTC()) || slot.BookedCount >= slot.Capacity {
		return models.Slot{}, models.Shop{}, ErrSlotNotBookable
	}

	loc, err := time.LoadLocation(shop.Timezone)
	if err != nil {
		return models.Slot{}, models.Shop{}, err
	}
	slotDate := utils.DateOnly(slot.StartsAt, loc)
	if err := utils.ValidateBookingWindow(slotDate, loc, now); err != nil {
		return models.Slot{}, models.Shop{}, ErrBookingWindow
	}

	var blockedCount int64
	if err := s.db.WithContext(ctx).Model(&models.BlockedDate{}).
		Where("shop_id = ? AND date = ?", shop.ID, slotDate).
		Count(&blockedCount).Error; err != nil {
		return models.Slot{}, models.Shop{}, err
	}
	if blockedCount > 0 {
		return models.Slot{}, models.Shop{}, ErrSlotNotBookable
	}

	var businessDay models.BusinessDay
	if err := s.db.WithContext(ctx).
		Where("shop_id = ? AND weekday = ? AND is_active = true", shop.ID, int(slot.StartsAt.In(loc).Weekday())).
		First(&businessDay).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Slot{}, models.Shop{}, ErrSlotNotBookable
		}
		return models.Slot{}, models.Shop{}, err
	}

	return slot, shop, nil
}

func (s *Service) uniqueBookingCode(ctx context.Context) (string, error) {
	for i := 0; i < 20; i++ {
		code, err := utils.GenerateBookingCode()
		if err != nil {
			return "", err
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.Booking{}).Where("code = ?", code).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return code, nil
		}
	}
	return "", fmt.Errorf("could not generate unique booking code")
}

func paymentReference(code string) (string, error) {
	random, err := utils.RandomString(8, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", code, random), nil
}

func MarshalProviderPayload(value interface{}) datatypes.JSON {
	payload, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(payload)
}
