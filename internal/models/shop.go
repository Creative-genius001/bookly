package models

import "github.com/google/uuid"

type Shop struct {
	BaseModel
	// OwnerID is a plain index (not unique): an owner may have multiple shops.
	OwnerID                uuid.UUID `json:"owner_id" gorm:"type:uuid;index;not null"`
	Owner                  User      `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Name                   string    `json:"name" gorm:"type:varchar(150);not null"`
	Slug                   string    `json:"slug" gorm:"type:varchar(180);uniqueIndex;not null"`
	Email                  string    `json:"email" gorm:"type:varchar(255);not null"`
	Phone                  string    `json:"phone" gorm:"type:varchar(40);not null"`
	Address                string    `json:"address" gorm:"type:varchar(255)"`
	Latitude               *float64  `json:"latitude" gorm:"type:double precision"`
	Longitude              *float64  `json:"longitude" gorm:"type:double precision"`
	LogoURL                string    `json:"logo_url" gorm:"type:varchar(500)"`
	CoverImageURL          string    `json:"cover_image_url" gorm:"type:varchar(500)"`
	Timezone               string    `json:"timezone" gorm:"type:varchar(80);not null;default:'UTC'"`
	IsActive               bool      `json:"is_active" gorm:"not null;default:true"`
	BarbingDurationMinutes int       `json:"barbing_duration" gorm:"not null;default:60"`
	CapacityPerSlot        int       `json:"capacity_per_slot" gorm:"not null;default:1"`
}
