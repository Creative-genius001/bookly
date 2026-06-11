package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"barber-booking-backend/internal/config"
	"barber-booking-backend/internal/database"
	"barber-booking-backend/internal/logging"
	"barber-booking-backend/internal/notifications"
	"barber-booking-backend/internal/paystack"
	"barber-booking-backend/internal/server"
	"barber-booking-backend/internal/storage"
)

func main() {
	_ = godotenv.Load()

	bootstrapLogger, err := logging.New(logging.Config{
		Environment: os.Getenv("APP_ENV"),
		Level:       os.Getenv("LOG_LEVEL"),
		Format:      os.Getenv("LOG_FORMAT"),
	})
	if err != nil {
		bootstrapLogger = slog.Default()
	}
	slog.SetDefault(bootstrapLogger)

	cfg, err := config.Load()
	if err != nil {
		fatal(bootstrapLogger, "failed to load config", err)
	}

	logger, err := logging.New(logging.Config{
		Environment: cfg.AppEnv,
		Level:       cfg.Logging.Level,
		Format:      cfg.Logging.Format,
		AddSource:   cfg.Logging.AddSource,
	})
	if err != nil {
		fatal(bootstrapLogger, "failed to initialize logger", err)
	}
	logger = logger.With("service", "barber-booking-api", "env", cfg.AppEnv)
	slog.SetDefault(logger)

	dbLogger := logging.NewGormLogger(logger, cfg.DatabaseSlowQuery, cfg.DatabaseLogSQL)
	db, err := database.ConnectPostgres(cfg.DatabaseURL, dbLogger)
	if err != nil {
		fatal(logger, "failed to connect postgres", err)
	}
	logger.Info("postgres connected")

	if err := database.AutoMigrate(db); err != nil {
		fatal(logger, "failed to auto migrate database", err)
	}
	logger.Info("database migrations completed")

	redisClient, err := database.ConnectRedis(cfg.Redis)
	if err != nil {
		fatal(logger, "failed to connect redis", err)
	}
	logger.Info("redis connected")
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Warn("failed to close redis", "error", err)
		}
	}()

	// Use real SMTP delivery when configured, otherwise log emails. Either way
	// delivery runs off the request path via the async decorator.
	var emailSender notifications.EmailSender
	if cfg.SMTP.Enabled() {
		emailSender = notifications.NewAsyncEmailSender(notifications.NewSMTPEmailSender(cfg.SMTP), logger)
		logger.Info("smtp email sender enabled", "host", cfg.SMTP.Host, "from", cfg.SMTP.From)
	} else {
		emailSender = notifications.NewLogEmailSender(logger)
		logger.Info("log email sender enabled (set SMTP_HOST and SMTP_FROM for real delivery)")
	}
	notifier := notifications.NewNotifier(emailSender)
	paystackClient := paystack.NewClient(cfg.Paystack)
	uploader := storage.NewCloudinaryUploader(cfg.Cloudinary)
	if uploader.Enabled() {
		logger.Info("cloudinary uploads enabled", "cloud", cfg.Cloudinary.CloudName)
	} else {
		logger.Info("cloudinary uploads disabled (set CLOUDINARY_* env to enable)")
	}

	router := server.NewRouter(server.Dependencies{
		Config:   cfg,
		DB:       db,
		Redis:    redisClient,
		Logger:   logger,
		Notifier: notifier,
		Paystack: paystackClient,
		Uploader: uploader,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErrors:
		fatal(logger, "server listen failed", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fatal(logger, "server shutdown failed", err)
	}
	logger.Info("server shutdown complete")
}

func fatal(logger *slog.Logger, message string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error(message, "error", err)
	os.Exit(1)
}
