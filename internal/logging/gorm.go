package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type GormLogger struct {
	logger        *slog.Logger
	level         gormlogger.LogLevel
	logSQL        bool
	slowThreshold time.Duration
}

func NewGormLogger(logger *slog.Logger, slowThreshold time.Duration, logSQL bool) gormlogger.Interface {
	if logger == nil {
		logger = slog.Default()
	}
	if slowThreshold <= 0 {
		slowThreshold = 500 * time.Millisecond
	}
	return GormLogger{
		logger:        logger.With("component", "gorm"),
		level:         gormlogger.Info,
		logSQL:        logSQL,
		slowThreshold: slowThreshold,
	}
}

func (l GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	l.level = level
	return l
}

func (l GormLogger) Info(ctx context.Context, message string, args ...interface{}) {
	if l.level >= gormlogger.Info {
		l.logger.InfoContext(ctx, formatGormMessage(message, args...))
	}
}

func (l GormLogger) Warn(ctx context.Context, message string, args ...interface{}) {
	if l.level >= gormlogger.Warn {
		l.logger.WarnContext(ctx, formatGormMessage(message, args...))
	}
}

func (l GormLogger) Error(ctx context.Context, message string, args ...interface{}) {
	if l.level >= gormlogger.Error {
		l.logger.ErrorContext(ctx, formatGormMessage(message, args...))
	}
}

func (l GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error {
		sql, rows := fc()
		attrs := l.queryAttrs(elapsed, rows, sql)
		attrs = append([]any{"error", err}, attrs...)
		l.logger.ErrorContext(ctx, "database query failed",
			attrs...,
		)
		return
	}

	if l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= gormlogger.Warn {
		sql, rows := fc()
		attrs := append(l.queryAttrs(elapsed, rows, sql),
			"slow_threshold_ms", elapsedMilliseconds(l.slowThreshold),
		)
		l.logger.WarnContext(ctx, "slow database query",
			attrs...,
		)
		return
	}

	if l.level >= gormlogger.Info && l.logger.Enabled(ctx, slog.LevelDebug) {
		sql, rows := fc()
		l.logger.DebugContext(ctx, "database query",
			l.queryAttrs(elapsed, rows, sql)...,
		)
	}
}

func elapsedMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func formatGormMessage(message string, args ...interface{}) string {
	if len(args) == 0 {
		return message
	}
	return fmt.Sprintf(message, args...)
}

func (l GormLogger) queryAttrs(elapsed time.Duration, rows int64, sql string) []any {
	attrs := []any{
		"elapsed_ms", elapsedMilliseconds(elapsed),
		"rows", rows,
	}
	if l.logSQL {
		attrs = append(attrs, "sql", sql)
	}
	return attrs
}
