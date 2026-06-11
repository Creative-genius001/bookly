package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

type AuthRepository interface {
	CreateUser(ctx context.Context, tx *gorm.DB, user *models.User) error
	WithTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error
	FindValidRefreshToken(ctx context.Context, hash string, now time.Time) (*models.RefreshToken, error)
	FindUserByID(ctx context.Context, id string) (*models.User, error)
	FindUserByEmail(ctx context.Context, email string) (*models.User, error)
	RevokeToken(ctx context.Context, tx *gorm.DB, token *models.RefreshToken) error
	StoreRefreshToken(ctx context.Context, tx *gorm.DB, token *models.RefreshToken) error
	Logout(ctx context.Context, userID string, refreshToken string) error

	CreatePasswordResetToken(ctx context.Context, token *models.PasswordResetToken) error
	FindValidPasswordResetToken(ctx context.Context, hash string, now time.Time) (*models.PasswordResetToken, error)
	MarkPasswordResetTokenUsed(ctx context.Context, tx *gorm.DB, id uuid.UUID) error
	UpdateUserPassword(ctx context.Context, tx *gorm.DB, userID uuid.UUID, passwordHash string) error

	CreateEmailVerificationToken(ctx context.Context, token *models.EmailVerificationToken) error
	FindValidEmailVerificationToken(ctx context.Context, hash string, now time.Time) (*models.EmailVerificationToken, error)
	MarkEmailVerificationTokenUsed(ctx context.Context, tx *gorm.DB, id uuid.UUID) error
	MarkUserEmailVerified(ctx context.Context, tx *gorm.DB, userID uuid.UUID) error
}

type GormAuthRepository struct {
	db *gorm.DB
}

func NewGormAuthRepository(db *gorm.DB) AuthRepository {
	return &GormAuthRepository{db: db}
}

func (r *GormAuthRepository) Logout(ctx context.Context, userID string, refreshToken string) error {
	query := r.db.Model(&models.RefreshToken{}).WithContext(ctx).Where("user_id = ? AND is_revoked = ?", userID, false)
	if refreshToken != "" {
		query = query.Where("token_hash = ?", utils.HashToken(refreshToken))
	}

	refreshTokenUpdate := (map[string]any{
		"is_revoked": true,
		"revoked_at": time.Now().UTC(),
	})
	if err := query.Updates(refreshTokenUpdate).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", "unable to logout user")
	}
	return nil
}

func (r *GormAuthRepository) FindUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).Find(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorMap.New(errorMap.CodeNotFound, "Auth Repository layer", "user not found")
		}
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", err.Error())
	}
	return &user, nil
}

func (r *GormAuthRepository) FindUserByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).Where("id = ?", id).Find(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorMap.New(errorMap.CodeNotFound, "Auth Repository layer", "user not found")
		}
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", err.Error())
	}
	return &user, nil
}

func (r *GormAuthRepository) FindValidRefreshToken(ctx context.Context, hash string, now time.Time) (*models.RefreshToken, error) {
	if hash == "" {
		return nil, nil
	}
	var token models.RefreshToken
	if err := r.db.Model(&models.RefreshToken{}).WithContext(ctx).Where("token_hash = ? AND expires_at > ? AND is_revoked = ?", hash, now, false).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorMap.New(errorMap.CodeNotFound, "Auth Repository layer", "invalid refresh token")
		}
		return nil, errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", err.Error())
	}
	return &token, nil
}

func (r *GormAuthRepository) RevokeToken(ctx context.Context, tx *gorm.DB, token *models.RefreshToken) error {
	if err := tx.WithContext(ctx).Model(&models.RefreshToken{}).Where("id = ?", token.ID).Update("is_revoked", true).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", "failed to revoke token")
	}
	return nil
}

func (r *GormAuthRepository) StoreRefreshToken(ctx context.Context, tx *gorm.DB, token *models.RefreshToken) error {
	if err := tx.WithContext(ctx).Create(token).Error; err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", "failed to store refresh token")
	}
	return nil
}

func (r *GormAuthRepository) CreateUser(ctx context.Context, tx *gorm.DB, user *models.User) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	if err := db.WithContext(ctx).Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return errorMap.New(errorMap.CodeAlreadyExists, "Auth Repository layer", "user with this email already exist")
		}
		errorMapErr := errorMap.Wrap(err, errorMap.CodeInternal, "Auth Repository layer", "failed to create user")
		return errorMapErr
	}
	return nil
}

func (r *GormAuthRepository) WithTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}
