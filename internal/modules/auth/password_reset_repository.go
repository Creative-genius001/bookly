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

func (r *GormAuthRepository) CreatePasswordResetToken(ctx context.Context, token *models.PasswordResetToken) error {
	if err := r.db.WithContext(ctx).Create(token).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: password reset", "failed to store reset token")
	}
	return nil
}

func (r *GormAuthRepository) FindValidPasswordResetToken(ctx context.Context, hash string, now time.Time) (*models.PasswordResetToken, error) {
	if hash == "" {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "Auth Repository: password reset", "invalid reset token")
	}
	var token models.PasswordResetToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ? AND used_at IS NULL", hash, now).
		First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errorMap.New(errorMap.CodeInvalidInput, "Auth Repository: password reset", "reset token is invalid or has expired")
	}
	if err != nil {
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: password reset", "failed to load reset token")
	}
	return &token, nil
}

func (r *GormAuthRepository) MarkPasswordResetTokenUsed(ctx context.Context, tx *gorm.DB, id uuid.UUID) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	now := time.Now().UTC()
	if err := db.WithContext(ctx).
		Model(&models.PasswordResetToken{}).
		Where("id = ?", id).
		Update("used_at", &now).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: password reset", "failed to mark token used")
	}
	return nil
}

func (r *GormAuthRepository) UpdateUserPassword(ctx context.Context, tx *gorm.DB, userID uuid.UUID, passwordHash string) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	if err := db.WithContext(ctx).
		Model(&models.User{}).
		Where("id = ?", userID).
		Update("password_hash", passwordHash).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository: password reset", "failed to update password")
	}
	return nil
}
