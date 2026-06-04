package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	Environment string
	Level       string
	Format      string
	AddSource   bool
}

type loggerContextKey struct{}

func New(cfg Config) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	format := strings.ToLower(strings.TrimSpace(cfg.Format))
	if format == "" {
		if strings.EqualFold(cfg.Environment, "production") {
			format = "json"
		} else {
			format = "text"
		}
	}

	opts := &slog.HandlerOptions{
		AddSource: cfg.AddSource,
		Level:     level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.LevelKey {
				attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
			}
			return attr
		},
	}

	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts)), nil
	case "text", "console":
		return slog.New(slog.NewTextHandler(os.Stdout, opts)), nil
	default:
		return nil, fmt.Errorf("LOG_FORMAT must be one of: json, text")
	}
}

func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger)
	if !ok || logger == nil {
		return slog.Default()
	}
	return logger
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error")
	}
}
