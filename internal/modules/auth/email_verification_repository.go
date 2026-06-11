package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"barber-booking-backend/internal/models"
	errorMap "barber-booking-backend/internal/utils/error"
)

func (r *GormAuthRepository) CreateEmailVerificationToken(ctx context.Context, token *models.EmailVerificationToken) error {
	if err := r.db.WithContext(ctx).Create(token).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: email verification", "failed to store verification token")
	}
	return nil
}

func (r *GormAuthRepository) FindValidEmailVerificationToken(ctx context.Context, hash string, now time.Time) (*models.EmailVerificationToken, error) {
	if hash == "" {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "Auth Repository: email verification", "invalid verification token")
	}
	var token models.EmailVerificationToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ? AND used_at IS NULL", hash, now).
		First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "Auth Repository: email verification", "verification token is invalid or has expired")
	}
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: email verification", "failed to load verification token")
	}
	return &token, nil
}

func (r *GormAuthRepository) MarkEmailVerificationTokenUsed(ctx context.Context, tx *gorm.DB, id uuid.UUID) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	now := time.Now().UTC()
	if err := db.WithContext(ctx).
		Model(&models.EmailVerificationToken{}).
		Where("id = ?", id).
		Update("used_at", &now).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: email verification", "failed to mark token used")
	}
	return nil
}

func (r *GormAuthRepository) MarkUserEmailVerified(ctx context.Context, tx *gorm.DB, userID uuid.UUID) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	if err := db.WithContext(ctx).
		Model(&models.User{}).
		Where("id = ?", userID).
		Update("email_verified", true).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: email verification", "failed to mark email verified")
	}
	return nil
}
