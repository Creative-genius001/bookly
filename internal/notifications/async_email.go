package notifications

import (
	"context"
	"log/slog"
	"time"
)

// AsyncEmailSender wraps an EmailSender and performs delivery on a background
// goroutine with its own timeout, so request handlers never block on email I/O.
// Failures are logged rather than surfaced to the caller.
type AsyncEmailSender struct {
	inner   EmailSender
	logger  *slog.Logger
	timeout time.Duration
}

func NewAsyncEmailSender(inner EmailSender, logger *slog.Logger) *AsyncEmailSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &AsyncEmailSender{
		inner:   inner,
		logger:  logger.With("component", "email_async"),
		timeout: 20 * time.Second,
	}
}

func (a *AsyncEmailSender) Send(_ context.Context, message EmailMessage) error {
	// Detach from the request context so delivery survives the response.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
		defer cancel()
		if err := a.inner.Send(ctx, message); err != nil {
			a.logger.ErrorContext(ctx, "email delivery failed",
				"subject", message.Subject, "error", err)
		}
	}()
	return nil
}
