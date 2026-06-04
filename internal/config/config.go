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
	DatabaseURL        string
	DatabaseLogSQL     bool
	DatabaseSlowQuery  time.Duration
	Logging            LoggingConfig
	Redis              RedisConfig
	JWT                JWTConfig
	Paystack           PaystackConfig
	BookingAmountKobo  int64
	RateLimitPerMinute int
}

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

	cfg := Config{
		AppEnv:            env("APP_ENV", "development"),
		Port:              env("PORT", "8080"),
		APIBaseURL:        env("API_BASE_URL", "http://localhost:8080"),
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
		BookingAmountKobo:  amountKobo,
		RateLimitPerMinute: rateLimit,
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
