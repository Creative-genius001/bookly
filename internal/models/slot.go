package models

import (
	"time"

	"github.com/google/uuid"
)

type SlotStatus string

const (
	SlotAvailable SlotStatus = "available"
	SlotFull      SlotStatus = "full"
	SlotBlocked   SlotStatus = "blocked"
	SlotExpired   SlotStatus = "expired"
)

type Slot struct {
	BaseModel
	ShopID      uuid.UUID  `json:"shop_id" gorm:"type:uuid;not null;uniqueIndex:idx_shop_slot_start"`
	Shop        Shop       `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	StartsAt    time.Time  `json:"starts_at" gorm:"not null;uniqueIndex:idx_shop_slot_start;index"`
	EndsAt      time.Time  `json:"ends_at" gorm:"not null;index"`
	Capacity    int        `json:"capacity" gorm:"not null;default:1"`
	BookedCount int        `json:"booked_count" gorm:"not null;default:0"`
	Status      SlotStatus `json:"status" gorm:"type:varchar(20);not null;default:'available';index"`
}
