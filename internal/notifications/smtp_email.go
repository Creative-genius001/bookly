package notifications

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"barber-booking-backend/internal/config"
)

// SMTPEmailSender delivers email over SMTP using the standard library. It
// supports implicit TLS (port 465) and STARTTLS (port 587/25). No third-party
// dependencies are required.
type SMTPEmailSender struct {
	cfg config.SMTPConfig
}

func NewSMTPEmailSender(cfg config.SMTPConfig) *SMTPEmailSender {
	return &SMTPEmailSender{cfg: cfg}
}

func (s *SMTPEmailSender) Send(ctx context.Context, message EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	msg := s.build(message)

	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	// Implicit TLS (typically port 465).
	if s.cfg.Port == 465 || (s.cfg.UseTLS && s.cfg.Port != 587) {
		return s.sendImplicitTLS(ctx, addr, auth, message.To, msg)
	}
	return s.sendStartTLS(ctx, addr, auth, message.To, msg)
}

func (s *SMTPEmailSender) sendStartTLS(ctx context.Context, addr string, auth smtp.Auth, to string, msg []byte) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = client.Quit() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	return s.deliver(client, auth, to, msg)
}

func (s *SMTPEmailSender) sendImplicitTLS(ctx context.Context, addr string, auth smtp.Auth, to string, msg []byte) error {
	d := tls.Dialer{Config: &tls.Config{ServerName: s.cfg.Host}, NetDialer: &net.Dialer{Timeout: 10 * time.Second}}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp tls dial: %w", err)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = client.Quit() }()

	return s.deliver(client, auth, to, msg)
}

func (s *SMTPEmailSender) deliver(client *smtp.Client, auth smtp.Auth, to string, msg []byte) error {
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	}
	if err := client.Mail(s.fromAddress()); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	return w.Close()
}

func (s *SMTPEmailSender) fromAddress() string {
	// Allow "Name <addr@host>" form; extract the bare address for MAIL FROM.
	from := s.cfg.From
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j >= 0 {
			return from[i+1 : i+j]
		}
	}
	return from
}

func (s *SMTPEmailSender) build(m EmailMessage) []byte {
	var b strings.Builder
	b.WriteString("From: " + s.cfg.From + "\r\n")
	b.WriteString("To: " + m.To + "\r\n")
	b.WriteString("Subject: " + m.Subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(m.Body)
	b.WriteString("\r\n")
	return []byte(b.String())
}
