package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	ErrSlotCapacityExceeded = errors.New("slot capacity exceeded")
)

type Service struct {
	db         *gorm.DB
	locker     *locks.RedisLocker
	logger     *slog.Logger
	paystack   *paystack.Client
	notifier   *notifications.Notifier
	amountKobo int64
}

type InitiateResult struct {
	Booking          BookingResponse `json:"booking"`
	Payment          models.Payment  `json:"payment"`
	AuthorizationURL string          `json:"authorization_url"`
	AccessCode       string          `json:"access_code"`
}

type BookingResponse struct {
	ID               uuid.UUID `json:"id"`
	Code             string    `json:"code"`
	ShopID           uuid.UUID `json:"shop_id"`
	ServiceID        uuid.UUID `json:"service_id"`
	CustomerName     string    `json:"customer_name"`
	CustomerEmail    string    `json:"customer_email"`
	Status           string    `json:"status"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	PaymentReference string    `json:"payment_reference"`
}

func NewService(db *gorm.DB, locker *locks.RedisLocker, logger *slog.Logger, paystackClient *paystack.Client, notifier *notifications.Notifier, amountKobo int64) *Service {
	return &Service{
		db:         db,
		locker:     locker,
		logger:     logger,
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

func (s *Service) findShop(ctx context.Context, shopID uuid.UUID) (*models.Shop, error) {
	var shop models.Shop
	if err := s.db.WithContext(ctx).Where("id = ?", shopID).First(&shop).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorMap.New(errorMap.CodeNotFound, "Booking Service", "shop not found")
		}
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Find Shop", "shop not found")
	}
	return &shop, nil
}

func (s *Service) Initiate(ctx context.Context, payload initiateRequest) (InitiateResult, error) {
	key := setLockKey(payload.ServiceID.String(), payload.StartTime)
	lock, err := s.locker.Acquire(ctx, key, 15*time.Second)
	if err != nil {
		return InitiateResult{}, errorMap.New(errorMap.CodeInternal, "Booking Service: Acquire slot lock", ErrSlotNotBookable.Error())
	}
	defer func() { _ = s.locker.Release(ctx, lock) }()

	service, err := s.findService(ctx, payload.ServiceID)
	if err != nil {
		return InitiateResult{}, err
	}

	shop, err := s.findShop(ctx, service.ShopID)
	if err != nil {
		return InitiateResult{}, err
	}
	endTime := payload.StartTime.Add(time.Duration(service.DurationInMinutes) * time.Minute)

	err = validateBookingTime(payload.StartTime, endTime, time.Now().UTC())
	if err != nil {
		return InitiateResult{}, err
	}

	ok, err := s.isSlotAvailable(ctx, shop.CapacityPerSlot, service.ShopID, payload.StartTime, endTime)
	if err != nil {
		return InitiateResult{}, err
	}
	if !ok {
		return InitiateResult{}, errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Check slot availability", ErrSlotCapacityExceeded.Error())
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
		ShopID:           service.ShopID,
		ServiceID:        payload.ServiceID,
		CustomerEmail:    payload.CustomerEmail,
		CustomerName:     payload.CustomerName,
		Status:           models.BookingPendingPayment,
		StartsAt:         payload.StartTime,
		EndsAt:           endTime,
		PaymentReference: reference,
	}
	payment := models.Payment{
		Reference:  reference,
		AmountKobo: int64(service.Price) * 100, // convert to kobo
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
		Amount:    payment.AmountKobo,
		Channels:  []paystack.Channel{paystack.ChannelCard, paystack.ChannelBankTransfer},
		Reference: reference,
		Metadata: map[string]any{
			"booking_code": booking.Code,
			"booking_id":   booking.ID.String(),
			"shop_id":      service.ShopID.String(),
			"service_id":   payload.ServiceID.String(),
		},
	})
	if err != nil {
		return InitiateResult{}, err
	}

	bookingResponse := BookingResponse{
		ID:               booking.ID,
		Code:             booking.Code,
		ShopID:           booking.ShopID,
		ServiceID:        booking.ServiceID,
		CustomerName:     booking.CustomerName,
		CustomerEmail:    booking.CustomerEmail,
		Status:           string(booking.Status),
		StartsAt:         booking.StartsAt,
		EndsAt:           booking.EndsAt,
		PaymentReference: booking.PaymentReference,
	}

	return InitiateResult{
		Booking:          bookingResponse,
		Payment:          payment,
		AuthorizationURL: init.AuthorizationURL,
		AccessCode:       init.AccessCode,
	}, nil
}

func (s *Service) InitializePayment(
	ctx context.Context,
	bookingCode string,
	paymentReferenceCode string,
) (InitiateResult, error) {

	var booking models.Booking
	err := s.db.WithContext(ctx).
		Where(
			"code = ? AND payment_reference = ? AND status = ?",
			bookingCode,
			paymentReferenceCode,
			models.BookingPendingPayment,
		).
		First(&booking).
		Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InitiateResult{}, errorMap.New(
				errorMap.CodeNotFound,
				"Booking Service",
				ErrBookingNotFound.Error(),
			)
		}

		return InitiateResult{}, errorMap.Wrap(
			err,
			errorMap.CodeInternal,
			"Booking Service",
			ErrBookingNotFound.Error(),
		)
	}

	// booking already passed
	if !booking.StartsAt.After(time.Now().UTC()) {
		_ = s.db.WithContext(ctx).
			Model(&booking).
			Update("status", models.BookingExpired).
			Error

		return InitiateResult{}, errorMap.New(
			errorMap.CodeInvalidInput,
			"Booking Service",
			"booking has already expired",
		)
	}

	// pending booking timeout (15 mins)
	if booking.CreatedAt.Add(15 * time.Minute).Before(time.Now().UTC()) {

		_ = s.db.WithContext(ctx).
			Model(&booking).
			Update("status", models.BookingExpired).
			Error

		return InitiateResult{}, errorMap.New(
			errorMap.CodeInvalidInput,
			"Booking Service",
			"booking payment session expired",
		)
	}

	var service models.Service

	if err := s.db.WithContext(ctx).
		First(&service, "id = ?", booking.ServiceID).
		Error; err != nil {

		return InitiateResult{}, errorMap.Wrap(
			err,
			errorMap.CodeInternal,
			"Booking Service",
			"unable to find service",
		)
	}

	var payment models.Payment

	err = s.db.WithContext(ctx).
		Where(
			"booking_id = ? AND status = ?",
			booking.ID,
			models.PaymentPending,
		).
		First(&payment).
		Error

	if err != nil {

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return InitiateResult{}, errorMap.Wrap(
				err,
				errorMap.CodeInternal,
				"Booking Service",
				"unable to find payment",
			)
		}

		reference, err := paymentReference(booking.Code)
		if err != nil {
			return InitiateResult{}, err
		}

		payment = models.Payment{
			BookingID:  booking.ID,
			Reference:  reference,
			AmountKobo: int64(service.Price) * 100,
			Status:     models.PaymentPending,
		}

		if err := s.db.WithContext(ctx).
			Create(&payment).
			Error; err != nil {

			return InitiateResult{}, errorMap.Wrap(
				err,
				errorMap.CodeInternal,
				"Booking Service",
				"unable to create payment",
			)
		}
	}

	init, err := s.paystack.InitializeTransaction(
		ctx,
		paystack.InitializeRequest{
			Email:     booking.CustomerEmail,
			Amount:    payment.AmountKobo,
			Reference: payment.Reference,
			Channels: []paystack.Channel{
				paystack.ChannelCard,
				paystack.ChannelBankTransfer,
			},
			Metadata: map[string]any{
				"booking_id":   booking.ID.String(),
				"booking_code": booking.Code,
				"service_id":   service.ID.String(),
				"shop_id":      booking.ShopID.String(),
			},
		},
	)

	if err != nil {
		return InitiateResult{}, err
	}

	bookingResponse := BookingResponse{
		ID:               booking.ID,
		Code:             booking.Code,
		ShopID:           booking.ShopID,
		ServiceID:        booking.ServiceID,
		CustomerName:     booking.CustomerName,
		CustomerEmail:    booking.CustomerEmail,
		Status:           string(booking.Status),
		StartsAt:         booking.StartsAt,
		EndsAt:           booking.EndsAt,
		PaymentReference: booking.PaymentReference,
	}

	return InitiateResult{
		Booking:          bookingResponse,
		Payment:          payment,
		AuthorizationURL: init.AuthorizationURL,
		AccessCode:       init.AccessCode,
	}, nil
}

func (s *Service) HandlePaymentSuccess(ctx context.Context, event paystack.WebhookEvent) error {
	reference := event.Data.Reference

	// Server-side verification — NEVER trust the webhook amount alone
	verified, err := s.paystack.Verify(ctx, reference)
	if err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Webhook", "could not verify transaction")
	}
	if verified.Status != "success" {
		// Paystack says it's not actually successful — treat as failed
		// email := event.Data.Customer.Email
		s.logger.DebugContext(ctx, "User payment is successful", event)
		// return s.handlePaymentFailed(ctx, email)
	}
	// if verified.AmountKobo != s.bookingPrice {
	// 	// Amount tampering — refund and reject
	// 	_ = s.paystack.Refund(ctx, paystack.RefundInput{TransactionReference: reference})
	// 	return nil
	// }

	var payment models.Payment
	if err := s.db.WithContext(ctx).Where("reference = ?", reference).First(&payment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // unknown reference — ignore
		}
		return errorMap.Wrap(err, errorMap.CodeInternal, "Webhook", "could not find payment")
	}

	// Acquire the slot lock — same lock used in Initiate()
	var bookingForLock models.Booking
	if err := s.db.WithContext(ctx).First(&bookingForLock, "id = ?", payment.BookingID).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Webhook", "could not find booking")
	}

	var confirmedBooking models.Booking
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p models.Payment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&p, "id = ?", payment.ID).Error; err != nil {
			return err
		}

		if p.Status == models.PaymentSuccess || p.Status == models.PaymentRefunded {
			return nil
		}

		var b models.Booking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&b, "id = ?", p.BookingID).Error; err != nil {
			return err
		}

		if err := tx.Model(&p).Updates(map[string]interface{}{
			"status":  models.PaymentSuccess,
			"channel": event.Data.Channel, // "card" or "bank_transfer"
			"paid_at": verified.PaidAt,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&b).Update("status", models.BookingConfirmed).Error; err != nil {
			return err
		}

		confirmedBooking = b
		return nil
	})
	if txErr != nil {
		return errorMap.Wrap(txErr, errorMap.CodeInternal, "Webhook", "transaction failed")
	}

	// Post-transaction side effects — outside the transaction so a notification
	// failure never rolls back the confirmed booking
	// if needsRefund {
	// 	_ = s.paystack.Refund(ctx, paystack.RefundInput{TransactionReference: reference})
	// 	s.notifier.SendBookingExpired(confirmedBooking)
	// 	return nil
	// }

	s.notifier.BookingConfirmed(ctx, confirmedBooking)
	s.notifier.PaymentSuccess(ctx, confirmedBooking)
	return nil
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

// func (s *Service) bookableSlot(ctx context.Context, payload initiateRequest, now time.Time) (models.Slot, models.Shop, error) {
// 	var service models.Service
// 	if err := s.db.WithContext(ctx).Where("id = ?", payload.ServiceID).First(&service).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			return models.Slot{}, models.Shop{}, ErrSlotNotBookable
// 		}
// 		return models.Slot{}, models.Shop{}, err
// 	}

// 	var shop models.Shop
// 	if err := s.db.WithContext(ctx).Where("id = ?", service.ShopID).First(&shop).Error; err != nil {
// 		return models.Slot{}, models.Shop{}, err
// 	}
// 	if !shop.IsActive || slot.Status == models.SlotBlocked || payload.StartTime.After(now.UTC()) || slot.BookedCount >= slot.Capacity {
// 		return models.Slot{}, models.Shop{}, ErrSlotNotBookable
// 	}

// 	loc, err := time.LoadLocation(shop.Timezone)
// 	if err != nil {
// 		return models.Slot{}, models.Shop{}, err
// 	}
// 	slotDate := utils.DateOnly(slot.StartsAt, loc)
// 	if err := utils.ValidateBookingWindow(slotDate, loc, now); err != nil {
// 		return models.Slot{}, models.Shop{}, ErrBookingWindow
// 	}

// 	var blockedCount int64
// 	if err := s.db.WithContext(ctx).Model(&models.BlockedDate{}).
// 		Where("shop_id = ? AND date = ?", shop.ID, slotDate).
// 		Count(&blockedCount).Error; err != nil {
// 		return models.Slot{}, models.Shop{}, err
// 	}
// 	if blockedCount > 0 {
// 		return models.Slot{}, models.Shop{}, ErrSlotNotBookable
// 	}

// 	var businessDay models.BusinessDay
// 	if err := s.db.WithContext(ctx).
// 		Where("shop_id = ? AND weekday = ? AND is_active = true", shop.ID, int(slot.StartsAt.In(loc).Weekday())).
// 		First(&businessDay).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			return models.Slot{}, models.Shop{}, ErrSlotNotBookable
// 		}
// 		return models.Slot{}, models.Shop{}, err
// 	}

// 	return slot, shop, nil
// }

func (s *Service) isSlotAvailable(ctx context.Context, capacity int, shopID uuid.UUID, startTime, endTime time.Time) (bool, error) {
	var count int64
	statuses := []string{string(models.BookingConfirmed), string(models.BookingPendingPayment)}
	if err := s.db.WithContext(ctx).Model(&models.Booking{}).
		Where("shop_id = ? AND status IN ? AND ((starts_at < ? AND ends_at > ?))",
			shopID,
			statuses,
			endTime, startTime,
		).
		Count(&count).Error; err != nil {
		return false, errorMap.Wrap(err, errorMap.CodeInternal, "Booking Service: Check slot availability", "unexpected error occured")
	}
	return count < int64(capacity), nil
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

func setLockKey(serviceId string, startTime time.Time) string {
	return fmt.Sprintf(
		"lock:booking:%s:%s",
		serviceId,
		startTime.UTC().Format(time.RFC3339),
	)
}

func validateBookingTime(
	startAt time.Time,
	endAt time.Time,
	now time.Time,
) error {

	start := startAt.UTC()
	current := now.UTC()

	if !start.After(current) {
		return errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Booking time past", "booking time is in the past")
	}

	if !endAt.After(current) {
		return errorMap.New(errorMap.CodeInvalidInput, "Booking Service: Booking expired", "booking already expired")
	}

	return nil
}

func MarshalProviderPayload(value interface{}) datatypes.JSON {
	payload, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(payload)
}
