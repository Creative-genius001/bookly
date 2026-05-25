package models

import (
	"time"

	"github.com/google/uuid"
)

type BusinessDay struct {
	BaseModel
	ShopID    uuid.UUID `json:"shop_id" gorm:"type:uuid;not null;uniqueIndex:idx_shop_weekday"`
	Shop      Shop      `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Weekday   int       `json:"weekday" gorm:"not null;uniqueIndex:idx_shop_weekday"`
	IsActive  bool      `json:"is_active" gorm:"not null;default:false"`
	OpenTime  string    `json:"open_time" gorm:"type:varchar(5);not null"`
	CloseTime string    `json:"close_time" gorm:"type:varchar(5);not null"`
}

type BlockedDate struct {
	BaseModel
	ShopID uuid.UUID `json:"shop_id" gorm:"type:uuid;not null;uniqueIndex:idx_shop_blocked_date"`
	Shop   Shop      `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Date   time.Time `json:"date" gorm:"type:date;not null;uniqueIndex:idx_shop_blocked_date"`
	Reason string    `json:"reason" gorm:"type:varchar(255)"`
}
