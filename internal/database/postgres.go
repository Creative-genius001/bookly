package database

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"barber-booking-backend/internal/models"
)

func ConnectPostgres(databaseURL string, dbLogger gormlogger.Interface) (*gorm.DB, error) {
	if dbLogger == nil {
		dbLogger = gormlogger.Default.LogMode(gormlogger.Warn)
	}

	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: dbLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.Shop{},
		&models.BusinessDay{},
		&models.BlockedDate{},
		&models.Slot{},
		&models.Booking{},
		&models.Payment{},
		&models.Service{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto migrate database: %w", err)
	}
	return nil
}
