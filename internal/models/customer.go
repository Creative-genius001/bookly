package models

import "github.com/google/uuid"

type Customer struct {
	BaseModel
	UserID   uuid.UUID `json:"user_id" gorm:"type:uuid;not null;uniqueIndex"`
	User     User      `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	FullName string    `json:"full_name" gorm:"type:varchar(150);not null"`
	Email    string    `json:"email" gorm:"type:varchar(255);not null"`
	Phone    string    `json:"phone" gorm:"type:varchar(40);not null"`
}
