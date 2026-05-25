package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidRole        = errors.New("role must be customer or owner")
	ErrInvalidRefresh     = errors.New("invalid refresh token")
)

type Service struct {
	db     *gorm.DB
	jwtCfg config.JWTConfig
}

type AuthResult struct {
	User   models.User     `json:"user"`
	Tokens utils.TokenPair `json:"tokens"`
}

func NewService(db *gorm.DB, jwtCfg config.JWTConfig) *Service {
	return &Service{db: db, jwtCfg: jwtCfg}
}

func (s *Service) Signup(ctx context.Context, email, phone, password string, role models.UserRole) (AuthResult, error) {
	if role != models.RoleCustomer && role != models.RoleOwner {
		return AuthResult{}, ErrInvalidRole
	}

	hash, err := utils.HashPassword(password)
	if err != nil {
		return AuthResult{}, err
	}

	user := models.User{
		Email:        strings.ToLower(strings.TrimSpace(email)),
		Phone:        strings.TrimSpace(phone),
		PasswordHash: hash,
		Role:         role,
	}

	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return AuthResult{}, fmt.Errorf("create user: %w", err)
	}

	tokens, err := s.issueTokens(ctx, user)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{User: user, Tokens: tokens}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (AuthResult, error) {
	var user models.User
	if err := s.db.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, err
	}

	if !utils.CheckPassword(user.PasswordHash, password) {
		return AuthResult{}, ErrInvalidCredentials
	}

	tokens, err := s.issueTokens(ctx, user)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{User: user, Tokens: tokens}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (AuthResult, error) {
	claims, err := utils.ParseJWT(refreshToken, s.jwtCfg.Secret, utils.TokenTypeRefresh)
	if err != nil {
		return AuthResult{}, ErrInvalidRefresh
	}

	tokenHash := utils.HashToken(refreshToken)

	var stored models.RefreshToken
	if err := s.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, time.Now().UTC()).
		First(&stored).Error; err != nil {
		return AuthResult{}, ErrInvalidRefresh
	}

	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", claims.UserID).Error; err != nil {
		return AuthResult{}, ErrInvalidRefresh
	}

	var result AuthResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&stored).Error; err != nil {
			return err
		}

		tokens, err := utils.GenerateTokenPair(user, s.jwtCfg)
		if err != nil {
			return err
		}
		if err := storeRefreshToken(ctx, tx, user.ID, tokens.RefreshToken, tokens.RefreshExpiresAt); err != nil {
			return err
		}

		result = AuthResult{User: user, Tokens: tokens}
		return nil
	})
	if err != nil {
		return AuthResult{}, err
	}

	return result, nil
}

func (s *Service) Logout(ctx context.Context, userID string, refreshToken string) error {
	query := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if refreshToken != "" {
		query = query.Where("token_hash = ?", utils.HashToken(refreshToken))
	}
	return query.Delete(&models.RefreshToken{}).Error
}

func (s *Service) issueTokens(ctx context.Context, user models.User) (utils.TokenPair, error) {
	tokens, err := utils.GenerateTokenPair(user, s.jwtCfg)
	if err != nil {
		return utils.TokenPair{}, err
	}
	if err := storeRefreshToken(ctx, s.db, user.ID, tokens.RefreshToken, tokens.RefreshExpiresAt); err != nil {
		return utils.TokenPair{}, err
	}
	return tokens, nil
}

func storeRefreshToken(ctx context.Context, db *gorm.DB, userID uuid.UUID, refreshToken string, expiresAt time.Time) error {
	record := models.RefreshToken{
		UserID:    userID,
		TokenHash: utils.HashToken(refreshToken),
		ExpiresAt: expiresAt,
	}
	return db.WithContext(ctx).Create(&record).Error
}
