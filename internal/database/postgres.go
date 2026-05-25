package database

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"barber-booking-backend/internal/models"
)

func ConnectPostgres(databaseURL string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.Shop{},
		&models.BusinessDay{},
		&models.BlockedDate{},
		&models.Slot{},
		&models.Booking{},
		&models.Payment{},
	)
}
