package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppEnv             string
	Port               string
	APIBaseURL         string
	FrontendURL        string
	DatabaseURL        string
	DatabaseLogSQL     bool
	DatabaseSlowQuery  time.Duration
	Logging            LoggingConfig
	Redis              RedisConfig
	JWT                JWTConfig
	Paystack           PaystackConfig
	SMTP               SMTPConfig
	Cloudinary         CloudinaryConfig
	BookingAmountKobo  int64
	RateLimitPerMinute int
	// Platform commission (percent) withheld from each booking before it is
	// credited to the shop's wallet. 0 = shops keep the full amount.
	PlatformFeePercent int
}

type CloudinaryConfig struct {
	CloudName string
	APIKey    string
	APISecret string
	Folder    string
}

func (c CloudinaryConfig) Enabled() bool {
	return c.CloudName != "" && c.APIKey != "" && c.APISecret != ""
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
}

// Enabled reports whether real SMTP delivery is configured.
func (s SMTPConfig) Enabled() bool { return s.Host != "" && s.From != "" }

type LoggingConfig struct {
	Level     string
	Format    string
	AddSource bool
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type JWTConfig struct {
	Secret          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type PaystackConfig struct {
	SecretKey   string
	BaseURL     string
	CallbackURL string
}

func Load() (Config, error) {
	logAddSource, err := envBool("LOG_ADD_SOURCE", false)
	if err != nil {
		return Config{}, err
	}

	dbSlowQueryMS, err := envInt("DB_SLOW_QUERY_THRESHOLD_MS", 500)
	if err != nil {
		return Config{}, err
	}

	dbLogSQL, err := envBool("DB_LOG_SQL", false)
	if err != nil {
		return Config{}, err
	}

	redisDB, err := envInt("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}

	accessMinutes, err := envInt("ACCESS_TOKEN_TTL_MINUTES", 15)
	if err != nil {
		return Config{}, err
	}

	refreshHours, err := envInt("REFRESH_TOKEN_TTL_HOURS", 720)
	if err != nil {
		return Config{}, err
	}

	amountKobo, err := envInt64("BOOKING_AMOUNT_KOBO", 500000)
	if err != nil {
		return Config{}, err
	}

	rateLimit, err := envInt("RATE_LIMIT_PER_MINUTE", 120)
	if err != nil {
		return Config{}, err
	}

	smtpPort, err := envInt("SMTP_PORT", 587)
	if err != nil {
		return Config{}, err
	}

	smtpUseTLS, err := envBool("SMTP_USE_TLS", false)
	if err != nil {
		return Config{}, err
	}

	platformFee, err := envInt("PLATFORM_FEE_PERCENT", 0)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:            env("APP_ENV", "development"),
		Port:              env("PORT", "8080"),
		APIBaseURL:        env("API_BASE_URL", "http://localhost:8080"),
		FrontendURL:       env("FRONTEND_URL", "http://localhost:3000"),
		DatabaseURL:       env("DATABASE_URL", "postgres://barber:barber@localhost:5432/barber_booking?sslmode=disable"),
		DatabaseLogSQL:    dbLogSQL,
		DatabaseSlowQuery: time.Duration(dbSlowQueryMS) * time.Millisecond,
		Logging: LoggingConfig{
			Level:     env("LOG_LEVEL", "info"),
			Format:    env("LOG_FORMAT", ""),
			AddSource: logAddSource,
		},
		Redis: RedisConfig{
			Addr:     env("REDIS_ADDR", "localhost:6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		JWT: JWTConfig{
			Secret:          env("JWT_SECRET", ""),
			AccessTokenTTL:  time.Duration(accessMinutes) * time.Minute,
			RefreshTokenTTL: time.Duration(refreshHours) * time.Hour,
		},
		Paystack: PaystackConfig{
			SecretKey:   env("PAYSTACK_SECRET_KEY", ""),
			BaseURL:     env("PAYSTACK_BASE_URL", "https://api.paystack.co"),
			CallbackURL: env("PAYSTACK_CALLBACK_URL", "http://localhost:3000/payment/callback"),
		},
		SMTP: SMTPConfig{
			Host:     env("SMTP_HOST", ""),
			Port:     smtpPort,
			Username: env("SMTP_USERNAME", ""),
			Password: env("SMTP_PASSWORD", ""),
			From:     env("SMTP_FROM", ""),
			UseTLS:   smtpUseTLS,
		},
		Cloudinary: CloudinaryConfig{
			CloudName: env("CLOUDINARY_CLOUD_NAME", ""),
			APIKey:    env("CLOUDINARY_API_KEY", ""),
			APISecret: env("CLOUDINARY_API_SECRET", ""),
			Folder:    env("CLOUDINARY_FOLDER", "bookly"),
		},
		BookingAmountKobo:  amountKobo,
		RateLimitPerMinute: rateLimit,
		PlatformFeePercent: platformFee,
	}

	if cfg.JWT.Secret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func envBool(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}
