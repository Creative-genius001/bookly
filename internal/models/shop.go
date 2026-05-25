package models

import "github.com/google/uuid"

type Shop struct {
	BaseModel
	OwnerID                uuid.UUID `json:"owner_id" gorm:"type:uuid;uniqueIndex;not null"`
	Owner                  User      `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Name                   string    `json:"name" gorm:"type:varchar(150);not null"`
	Slug                   string    `json:"slug" gorm:"type:varchar(180);uniqueIndex;not null"`
	Email                  string    `json:"email" gorm:"type:varchar(255);not null"`
	Phone                  string    `json:"phone" gorm:"type:varchar(40);not null"`
	Timezone               string    `json:"timezone" gorm:"type:varchar(80);not null;default:'UTC'"`
	IsActive               bool      `json:"is_active" gorm:"not null;default:true"`
	BarbingDurationMinutes int       `json:"barbing_duration" gorm:"not null;default:60"`
	CapacityPerSlot        int       `json:"capacity_per_slot" gorm:"not null;default:1"`
}
