package models

import (
	"time"

	"github.com/google/uuid"
)

type PaymentStatus string

type Channel string

const (
	ChannelCard         Channel = "card"
	ChannelBankTransfer Channel = "bank_transfer"
)

const (
	PaymentPending  PaymentStatus = "pending"
	PaymentSuccess  PaymentStatus = "success"
	PaymentFailed   PaymentStatus = "failed"
	PaymentRefunded PaymentStatus = "refunded"
)

type Payment struct {
	BaseModel
	BookingID  uuid.UUID     `json:"booking_id" gorm:"type:uuid;not null;index"`
	Booking    Booking       `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Reference  string        `json:"reference" gorm:"type:varchar(120);uniqueIndex;not null"`
	AmountKobo int64         `json:"amount_kobo" gorm:"not null"`
	Status     PaymentStatus `json:"status" gorm:"type:varchar(20);not null;index"`
	Channel    Channel       `json:"channel" gorm:"type:varchar(50);not null"`
	PaidAt     time.Time     `json:"paid_at"`
}
