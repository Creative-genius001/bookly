package models

import (
	"time"

	"github.com/google/uuid"
)

type PasswordResetToken struct {
	BaseModel
	UserID    uuid.UUID  `json:"user_id" gorm:"type:uuid;not null;index"`
	User      User       `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	TokenHash string     `json:"-" gorm:"type:char(64);uniqueIndex;not null"`
	ExpiresAt time.Time  `json:"expires_at" gorm:"not null"`
	UsedAt    *time.Time `json:"used_at"`
}
