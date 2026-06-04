package models

import (
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	BaseModel
	UserID    uuid.UUID  `json:"user_id" gorm:"type:uuid;not null;index"`
	User      User       `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	TokenHash string     `json:"-" gorm:"type:char(64);uniqueIndex;not null"`
	ExpiresAt time.Time  `json:"expires_at" gorm:"not null;"`
	IsRevoked bool       `json:"is_revoked" gorm:"type:boolean;default:false;"`
	RevokedAt *time.Time `json:"revoked_at"`
}
