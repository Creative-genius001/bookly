package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"barber-booking-backend/internal/models"
	"barber-booking-backend/internal/utils"
	errorMap "barber-booking-backend/internal/utils/error"
)

const (
	verifyTokenTTL    = 10 * time.Minute
	verifyTokenLength = 48
)

func (s *Service) issueEmailVerification(ctx context.Context, user models.User) error {
	raw, err := utils.RandomString(verifyTokenLength, resetTokenAlphabet)
	if err != nil {
		return err
	}
	token := &models.EmailVerificationToken{
		UserID:    user.ID,
		TokenHash: utils.HashToken(raw),
		ExpiresAt: time.Now().UTC().Add(verifyTokenTTL),
	}
	if err := s.repo.CreateEmailVerificationToken(ctx, token); err != nil {
		return err
	}

	link := strings.TrimRight(s.frontendURL, "/") + "/verify-email?token=" + raw
	if s.notifier != nil {
		_ = s.notifier.VerifyEmail(ctx, user.Email, link)
	}
	s.logger.InfoContext(ctx, "email verification link issued", "userID", user.ID)
	return nil
}

// VerifyEmail consumes a verification token and marks the user verified.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return errorMap.New(errorMap.CodeInvalidInput, "Auth: VerifyEmail", "verification token is required")
	}

	token, err := s.repo.FindValidEmailVerificationToken(ctx, utils.HashToken(rawToken), time.Now().UTC())
	if err != nil {
		return err
	}

	err = s.repo.WithTransaction(ctx, func(tx *gorm.DB) error {
		if err := s.repo.MarkUserEmailVerified(ctx, tx, token.UserID); err != nil {
			return err
		}
		return s.repo.MarkEmailVerificationTokenUsed(ctx, tx, token.ID)
	})
	if err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "email verified", "userID", token.UserID)
	return nil
}

// ResendVerification re-issues a verification email. Always succeeds from the
// caller's view (no email enumeration); a no-op if already verified.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}

	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) && appErr.Code == errorMap.CodeNotFound {
			return nil
		}
		return err
	}
	// FindUserByEmail uses Find(), so a missing user comes back zero-valued.
	if user == nil || user.ID == (models.User{}).ID || user.Email == "" {
		return nil
	}
	if user.EmailVerified {
		return nil
	}
	if err := s.issueEmailVerification(ctx, *user); err != nil {
		return err
	}
	return nil
}
