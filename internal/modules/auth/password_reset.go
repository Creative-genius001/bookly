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
	resetTokenTTL      = time.Hour
	resetTokenLength   = 48
	resetTokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// ForgotPassword issues a password reset token and emails (logs) a reset link.
// It always succeeds from the caller's perspective so a caller cannot probe
// which emails are registered.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}

	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		var appErr *errorMap.AppError
		if errors.As(err, &appErr) && appErr.Code == errorMap.CodeNotFound {
			// Unknown email — silently succeed.
			s.logger.InfoContext(ctx, "password reset requested for unknown email")
			return nil
		}
		return err
	}

	raw, err := utils.RandomString(resetTokenLength, resetTokenAlphabet)
	if err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth: ForgotPassword", "could not generate reset token")
	}

	token := &models.PasswordResetToken{
		UserID:    user.ID,
		TokenHash: utils.HashToken(raw),
		ExpiresAt: time.Now().UTC().Add(resetTokenTTL),
	}
	if err := s.repo.CreatePasswordResetToken(ctx, token); err != nil {
		return err
	}

	link := strings.TrimRight(s.frontendURL, "/") + "/reset-password?token=" + raw
	if s.notifier != nil {
		_ = s.notifier.PasswordReset(ctx, user.Email, link)
	}
	s.logger.InfoContext(ctx, "password reset link issued", "userID", user.ID)
	return nil
}

// ResetPassword validates a reset token, updates the password, marks the token
// used and revokes all of the user's refresh tokens.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return errorMap.New(errorMap.CodeInvalidInput, "Auth: ResetPassword", "reset token is required")
	}
	if err := utils.ValidatePassword(newPassword); err != nil {
		return errorMap.New(errorMap.CodeInvalidInput, "Auth: ResetPassword", err.Error())
	}

	token, err := s.repo.FindValidPasswordResetToken(ctx, utils.HashToken(rawToken), time.Now().UTC())
	if err != nil {
		return err
	}

	hash, err := utils.HashPassword(newPassword)
	if err != nil {
		return errorMap.Wrap(err, errorMap.CodeInternal, "Auth: ResetPassword", "could not hash password")
	}

	err = s.repo.WithTransaction(ctx, func(tx *gorm.DB) error {
		if err := s.repo.UpdateUserPassword(ctx, tx, token.UserID, hash); err != nil {
			return err
		}
		return s.repo.MarkPasswordResetTokenUsed(ctx, tx, token.ID)
	})
	if err != nil {
		return err
	}

	// Revoke existing sessions so a leaked token can't keep access.
	if err := s.repo.Logout(ctx, token.UserID.String(), ""); err != nil {
		s.logger.WarnContext(ctx, "failed to revoke sessions after password reset", "userID", token.UserID, "error", err)
	}

	s.logger.InfoContext(ctx, "password reset completed", "userID", token.UserID)
	return nil
}
