package models

type UserRole string

const (
	RoleCustomer UserRole = "customer"
	RoleOwner    UserRole = "owner"
)

type User struct {
	BaseModel
	Email        string   `json:"email" gorm:"type:varchar(255);uniqueIndex;not null"`
	Phone        string   `json:"phone" gorm:"type:varchar(40);uniqueIndex;not null"`
	PasswordHash string   `json:"-" gorm:"type:text;not null"`
	Role         UserRole `json:"role" gorm:"type:varchar(20);not null;index"`
}
