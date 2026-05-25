package models

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type PaymentStatus string

const (
	PaymentPending  PaymentStatus = "pending"
	PaymentSuccess  PaymentStatus = "success"
	PaymentFailed   PaymentStatus = "failed"
	PaymentRefunded PaymentStatus = "refunded"
)

type Payment struct {
	BaseModel
	BookingID       uuid.UUID      `json:"booking_id" gorm:"type:uuid;not null;index"`
	Booking         Booking        `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Reference       string         `json:"reference" gorm:"type:varchar(120);uniqueIndex;not null"`
	AmountKobo      int64          `json:"amount_kobo" gorm:"not null"`
	Status          PaymentStatus  `json:"status" gorm:"type:varchar(20);not null;index"`
	ProviderPayload datatypes.JSON `json:"provider_payload"`
}
