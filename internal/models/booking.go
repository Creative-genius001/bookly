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
	ServiceID        uuid.UUID     `json:"service_id" gorm:"type:uuid;not null;index"`
	Service          Service       `json:"service" gorm:"constraint:OnDelete:RESTRICT;"`
	CustomerName     string        `json:"customer_name" gorm:"type:varchar(100);not null"`
	CustomerEmail    string        `json:"customer_email" gorm:"type:varchar(100);not null"`
	Status           BookingStatus `json:"status" gorm:"type:varchar(30);not null;index"`
	StartsAt         time.Time     `json:"starts_at" gorm:"not null;index"`
	EndsAt           time.Time     `json:"ends_at" gorm:"not null"`
	PaymentReference string        `json:"payment_reference" gorm:"type:varchar(120);index"`
}
