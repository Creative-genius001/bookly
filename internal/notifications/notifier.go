package notifications

import (
	"context"
	"fmt"

	"barber-booking-backend/internal/models"
)

type Notifier struct {
	email EmailSender
}

func NewNotifier(email EmailSender) *Notifier {
	return &Notifier{email: email}
}

func (n *Notifier) BookingConfirmed(ctx context.Context, booking models.Booking) error {
	return n.email.Send(ctx, EmailMessage{
		To:      booking.CustomerEmail,
		Subject: "Booking confirmed",
		Body:    fmt.Sprintf("Your booking %s is confirmed for %s.", booking.Code, booking.StartsAt.Format("2006-01-02 15:04")),
	})
}

func (n *Notifier) BookingCancelled(ctx context.Context, booking models.Booking) error {
	return n.email.Send(ctx, EmailMessage{
		To:      booking.CustomerEmail,
		Subject: "Booking cancelled",
		Body:    fmt.Sprintf("Your booking %s has been cancelled.", booking.Code),
	})
}

func (n *Notifier) BookingRescheduled(ctx context.Context, user models.User, booking models.Booking) error {
	return n.email.Send(ctx, EmailMessage{
		To:      user.Email,
		Subject: "Booking rescheduled",
		Body:    fmt.Sprintf("Your booking %s has been rescheduled to %s.", booking.Code, booking.StartsAt.Format("2006-01-02 15:04")),
	})
}

func (n *Notifier) PaymentSuccess(ctx context.Context, booking models.Booking) error {
	return n.email.Send(ctx, EmailMessage{
		To:      booking.CustomerEmail,
		Subject: "Payment received",
		Body:    fmt.Sprintf("Payment for booking %s was successful.", booking.Code),
	})
}

func (n *Notifier) PaymentFailed(ctx context.Context, email string) error {
	return nil
	// return n.email.Send(ctx, EmailMessage{
	// 	To:      email,
	// 	Subject: "Payment failed",
	// 	Body:    fmt.Sprintf("Payment for booking %s failed.", booking.Code),
	// })
}

func (n *Notifier) RefundProcessed(ctx context.Context, user models.User, booking models.Booking) error {
	return n.email.Send(ctx, EmailMessage{
		To:      user.Email,
		Subject: "Refund processed",
		Body:    fmt.Sprintf("A refund has been processed for booking %s.", booking.Code),
	})
}
