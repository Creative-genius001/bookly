package models

import "github.com/google/uuid"

type Service struct {
	BaseModel
	ShopID            uuid.UUID `json:"shop_id" gorm:"type:uuid;not null;index"`
	Shop              Shop      `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Name              string    `json:"name" gorm:"type:varchar(150);not null"`
	Description       string    `json:"description" gorm:"type:varchar(255)"`
	Price             int       `json:"price" gorm:"not null;default:0"`
	DurationInMinutes int       `json:"duration_in_minutes" gorm:"not null;default:60"` // Duration in minutes
	IsActive          bool      `json:"is_active" gorm:"not null;default:true"`
}
