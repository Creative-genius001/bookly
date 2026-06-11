package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/paystack"
	errorMap "barber-booking-backend/internal/utils/error"
)

// HandleWebhook processes Paystack transfer.* events. Charge events are ignored
// here (the bookings module handles those). Safe to call for every webhook.
func (s *Service) HandleWebhook(ctx context.Context, body []byte, signature string) error {
	if !s.paystack.ValidSignature(body, signature) {
		return errorMap.New(errorMap.CodeUnauthorized, "Payouts Webhook", "invalid paystack signature")
	}
	var event paystack.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return errorMap.Wrap(err, errorMap.CodeInvalidInput, "Payouts Webhook", "invalid payload")
	}
	if !strings.HasPrefix(event.Event, "transfer.") {
		return nil
	}
	return s.handleTransferEvent(ctx, event)
}

func (s *Service) handleTransferEvent(ctx context.Context, event paystack.WebhookEvent) error {
	ref := event.Data.Reference
	if ref == "" {
		return nil
	}

	var request models.WithdrawalRequest
	err := s.db.WithContext(ctx).Where("reference = ?", ref).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil // unknown transfer — ignore
	}
	if err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Payouts Webhook", "could not load withdrawal")
	}

	switch event.Event {
	case "transfer.success":
		if request.Status == models.WithdrawalPaid {
			return nil
		}
		now := time.Now().UTC()
		if err := s.db.WithContext(ctx).Model(&models.WithdrawalRequest{}).
			Where("id = ?", request.ID).
			Updates(map[string]interface{}{
				"status":       models.WithdrawalPaid,
				"processed_at": &now,
			}).Error; err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Payouts Webhook", "could not mark paid")
		}
		s.logger.InfoContext(ctx, "withdrawal paid", "reference", ref)

	case "transfer.failed", "transfer.reversed":
		if request.Status == models.WithdrawalFailed {
			return nil
		}
		s.reverseWithdrawal(ctx, request, event.Event)
		s.logger.WarnContext(ctx, "withdrawal failed/reversed", "reference", ref, "event", event.Event)
	}
	return nil
}
