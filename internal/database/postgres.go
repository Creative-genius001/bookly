package database

import (
	"fmt"
	"time"

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
		Logger:                 dbLogger,
		PrepareStmt:            true, // cache prepared statements for hot queries
		SkipDefaultTransaction: true, // we wrap multi-write ops in explicit txns
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Tune the connection pool so we don't exhaust Postgres under load.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to access sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	// PostGIS powers the "nearest shop" discovery query (ST_DistanceSphere).
	// Best-effort: requires a DB role allowed to create extensions; if it fails,
	// discovery will error at query time until PostGIS is installed.
	_ = db.Exec(`CREATE EXTENSION IF NOT EXISTS postgis`).Error

	// Multi-shop: drop the legacy UNIQUE index on shops.owner_id so an owner can
	// have more than one shop. No-op on a fresh database. AutoMigrate then
	// (re)creates a plain, non-unique index from the model tag.
	if db.Migrator().HasTable(&models.Shop{}) {
		_ = db.Exec(`DROP INDEX IF EXISTS idx_shops_owner_id`).Error
	}

	err := db.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.PasswordResetToken{},
		&models.EmailVerificationToken{},
		&models.Shop{},
		&models.BusinessDay{},
		&models.BlockedDate{},
		&models.Booking{},
		&models.Payment{},
		&models.Service{},
		&models.ShopBankAccount{},
		&models.WalletEntry{},
		&models.WithdrawalRequest{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto migrate database: %w", err)
	}
	return nil
}
