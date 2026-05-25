package notifications

import (
	"context"
	"log"
)

type EmailMessage struct {
	To      string
	Subject string
	Body    string
}

type EmailSender interface {
	Send(ctx context.Context, message EmailMessage) error
}

type LogEmailSender struct{}

func NewLogEmailSender() *LogEmailSender {
	return &LogEmailSender{}
}

func (s *LogEmailSender) Send(_ context.Context, message EmailMessage) error {
	log.Printf("email notification to=%s subject=%q body=%q", message.To, message.Subject, message.Body)
	return nil
}
