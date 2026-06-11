package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/notifications"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrConflict           = errors.New("user already exist")
	ErrInvalidRole        = errors.New("role is invalid")
	ErrInvalidRefresh     = errors.New("invalid refresh token")
)

type Service struct {
	db          *gorm.DB
	jwtCfg      config.JWTConfig
	repo        AuthRepository
	logger      *slog.Logger
	notifier    *notifications.Notifier
	frontendURL string
}

type AuthResult struct {
	User   models.UserResponse `json:"user"`
	Tokens utils.TokenPair     `json:"tokens"`
}

func NewService(db *gorm.DB, jwtCfg config.JWTConfig, repo AuthRepository, logger *slog.Logger, notifier *notifications.Notifier, frontendURL string) *Service {
	return &Service{
		db:          db,
		jwtCfg:      jwtCfg,
		repo:        repo,
		logger:      logger,
		notifier:    notifier,
		frontendURL: frontendURL,
	}
}

func (s *Service) Signup(ctx context.Context, email, phone, password string, role models.UserRole) (AuthResult, error) {
	if role != models.RoleOwner {
		return AuthResult{}, errorMap.New(errorMap.CodeInvalidInput, "Signup Layer", ErrInvalidRole.Error())
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if err := utils.ValidateEmail(email); err != nil {
		return AuthResult{}, errorMap.New(errorMap.CodeInvalidInput, "Signup Layer", err.Error())
	}
	phone = strings.TrimSpace(phone)
	if err := utils.ValidatePassword(password); err != nil {
		return AuthResult{}, errorMap.New(errorMap.CodeInvalidInput, "Signup Layer", err.Error())
	}

	userExist, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return AuthResult{}, err
	}
	if userExist != nil {
		if userExist.Email == email {
			return AuthResult{}, errorMap.New(errorMap.CodeAlreadyExists, "Signup Layer", "user with this email already exist")
		}
	}

	hash, err := utils.HashPassword(password)
	if err != nil {
		return AuthResult{}, errorMap.Wrap(err, errorMap.CodeInternal, "Signup Layer", "could not hash password")
	}

	user := models.User{
		Email:        email,
		Phone:        phone,
		PasswordHash: hash,
		Role:         role,
	}

	var result AuthResult
	err = s.repo.WithTransaction(ctx, func(tx *gorm.DB) error {
		if err := s.repo.CreateUser(ctx, tx, &user); err != nil {
			return err
		}

		tokens, err := s.issueTokens(ctx, tx, user)
		if err != nil {
			return err
		}

		userResponse := models.UserResponse{
			ID:            user.ID,
			Email:         user.Email,
			Phone:         user.Phone,
			Role:          string(user.Role),
			EmailVerified: user.EmailVerified,
		}

		result = AuthResult{
			User:   userResponse,
			Tokens: tokens,
		}

		return nil
	})

	if err != nil {
		return AuthResult{}, err
	}

	// Send the verification email outside the signup transaction so a delivery
	// failure never blocks account creation.
	if err := s.issueEmailVerification(ctx, user); err != nil {
		s.logger.WarnContext(ctx, "could not issue email verification", "userID", user.ID, "error", err)
	}

	return result, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (AuthResult, error) {
	userExist, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return AuthResult{}, err
	}

	if !utils.CheckPassword(userExist.PasswordHash, password) {
		return AuthResult{}, errorMap.New(errorMap.CodeInvalidInput, "Login Layer", ErrInvalidCredentials.Error())
	}

	var tx *gorm.DB
	tokens, err := s.issueTokens(ctx, tx, *userExist)
	if err != nil {
		return AuthResult{}, err
	}

	s.logger.DebugContext(ctx, "tokens issued", "email", email, "userID", userExist.ID)

	userResponse := models.UserResponse{
		ID:            userExist.ID,
		Email:         userExist.Email,
		Phone:         userExist.Phone,
		Role:          string(userExist.Role),
		EmailVerified: userExist.EmailVerified,
	}

	return AuthResult{User: userResponse, Tokens: tokens}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (AuthResult, error) {
	claims, err := utils.ParseJWT(refreshToken, s.jwtCfg.Secret, utils.TokenTypeRefresh)
	if err != nil {
		return AuthResult{}, err
	}

	tokenHash := utils.HashToken(refreshToken)
	now := time.Now().UTC()

	stored, err := s.repo.FindValidRefreshToken(ctx, tokenHash, now)
	if err != nil {
		return AuthResult{}, err
	}

	user, err := s.repo.FindUserByID(ctx, claims.UserID)
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) && appErr.Code == errorMap.CodeNotFound {
			return AuthResult{}, errorMap.New(errorMap.CodeInvalidInput, "Auth Service layer", "invalid refresh token")
		}
		return AuthResult{}, err
	}

	var result AuthResult

	err = s.repo.WithTransaction(ctx, func(tx *gorm.DB) error {
		if err := s.repo.RevokeToken(ctx, tx, stored); err != nil {
			return err
		}

		tokens, err := utils.GenerateTokenPair(*user, s.jwtCfg)
		if err != nil {
			return errorMap.Wrap(err, errorMap.CodeInternal, "Auth Service layer", "unable to generate tokens")
		}

		newToken := &models.RefreshToken{
			UserID:    user.ID,
			TokenHash: utils.HashToken(tokens.RefreshToken),
			ExpiresAt: tokens.RefreshExpiresAt,
		}

		if err := s.repo.StoreRefreshToken(ctx, tx, newToken); err != nil {
			return err
		}

		result = AuthResult{
			User: models.UserResponse{
				ID:            user.ID,
				Email:         user.Email,
				Phone:         user.Phone,
				Role:          string(user.Role),
				EmailVerified: user.EmailVerified,
			},
			Tokens: tokens,
		}
		return nil
	})

	if err != nil {
		return AuthResult{}, err
	}

	return result, nil
}

func (s *Service) Logout(ctx context.Context, userID string, refreshToken string) error {
	s.logger.DebugContext(ctx, "logging out user", "userID", userID, "refreshToken", refreshToken)
	return s.repo.Logout(ctx, userID, refreshToken)
}

func (s *Service) issueTokens(ctx context.Context, tx *gorm.DB, user models.User) (utils.TokenPair, error) {
	tokens, err := utils.GenerateTokenPair(user, s.jwtCfg)
	if err != nil {
		return utils.TokenPair{}, errorMap.Wrap(err, errorMap.CodeInternal, "Issue token layer", "unable to generate tokens")
	}
	if err := s.StoreRefreshToken(ctx, tx, user.ID, tokens.RefreshToken, tokens.RefreshExpiresAt); err != nil {
		s.logger.ErrorContext(ctx, "failed to store refresh token", "error", err)
		return utils.TokenPair{}, errorMap.Wrap(err, errorMap.CodeInternal, "Issue token layer", "unable to store refresh tokens")
	}
	return tokens, nil
}

func (s *Service) StoreRefreshToken(ctx context.Context, tx *gorm.DB, userID uuid.UUID, refreshToken string, expiresAt time.Time) error {
	record := models.RefreshToken{
		UserID:    userID,
		TokenHash: utils.HashToken(refreshToken),
		ExpiresAt: expiresAt,
	}

	db := tx

	if db == nil {
		db = s.db
	}

	return db.WithContext(ctx).Create(&record).Error
}
