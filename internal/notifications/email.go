package notifications

import (
	"context"
	"log/slog"
	"strings"

	"barber-booking-backend/internal/logging"
)

type EmailMessage struct {
	To      string
	Subject string
	Body    string
}

type EmailSender interface {
	Send(ctx context.Context, message EmailMessage) error
}

type LogEmailSender struct {
	logger *slog.Logger
}

func NewLogEmailSender(logger *slog.Logger) *LogEmailSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogEmailSender{logger: logger.With("component", "email")}
}

func (s *LogEmailSender) Send(ctx context.Context, message EmailMessage) error {
	logger := s.logger
	if requestLogger := logging.FromContext(ctx); requestLogger != slog.Default() {
		logger = requestLogger.With("component", "email")
	}
	logger.InfoContext(ctx, "email notification queued",
		"to", maskEmail(message.To),
		"subject", message.Subject,
		"body_bytes", len(message.Body),
	)
	return nil
}

func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return ""
	}
	return parts[0][:1] + "***@" + parts[1]
}
