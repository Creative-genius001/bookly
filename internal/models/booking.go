package models

import (
	"time"

	"github.com/google/uuid"
)

type BookingStatus string

const (
	BookingPendingPayment BookingStatus = "pending_payment"
	BookingConfirmed      BookingStatus = "confirmed"
	BookingCancelled      BookingStatus = "cancelled"
	BookingRefunded       BookingStatus = "refunded"
	BookingExpired        BookingStatus = "expired"
)

type Booking struct {
	BaseModel
	Code             string        `json:"code" gorm:"type:varchar(20);uniqueIndex;not null"`
	ShopID           uuid.UUID     `json:"shop_id" gorm:"type:uuid;not null;index"`
	Shop             Shop          `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	SlotID           uuid.UUID     `json:"slot_id" gorm:"type:uuid;not null;index"`
	Slot             Slot          `json:"slot" gorm:"constraint:OnDelete:RESTRICT;"`
	CustomerID       uuid.UUID     `json:"customer_id" gorm:"type:uuid;not null;index"`
	Customer         User          `json:"-" gorm:"constraint:OnDelete:RESTRICT;"`
	Status           BookingStatus `json:"status" gorm:"type:varchar(30);not null;index"`
	StartsAt         time.Time     `json:"starts_at" gorm:"not null;index"`
	EndsAt           time.Time     `json:"ends_at" gorm:"not null"`
	RescheduledCount int           `json:"rescheduled_count" gorm:"not null;default:0"`
	CancelledAt      *time.Time    `json:"cancelled_at"`
	PaymentReference string        `json:"payment_reference" gorm:"type:varchar(120);index"`
}
