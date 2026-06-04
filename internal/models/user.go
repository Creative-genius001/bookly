package models

import "github.com/google/uuid"

type UserRole string

const (
	RoleCustomer UserRole = "customer"
	RoleOwner    UserRole = "owner"
)

type User struct {
	BaseModel
	Email        string   `json:"email" gorm:"type:varchar(255);uniqueIndex;not null"`
	Phone        string   `json:"phone" gorm:"type:varchar(40);index;not null"`
	PasswordHash string   `json:"-" gorm:"type:text;not null"`
	Role         UserRole `json:"role" gorm:"type:varchar(20);not null;index"`
}

type UserResponse struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Phone string    `json:"phone"`
	Role  string    `json:"role"`
}
