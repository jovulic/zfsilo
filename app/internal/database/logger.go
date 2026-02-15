package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	slogctx "github.com/veqryn/slog-context"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

// SlogContextAdapter implements gorm.Logger.
type SlogContextAdapter struct {
	SlowThreshold time.Duration
}

// Helper to safely get logger from context or fallback to the default.
func (s *SlogContextAdapter) getLogger(ctx context.Context) *slog.Logger {
	l := slogctx.FromCtx(ctx)
	if l != nil {
		return l
	}
	return slog.Default()
}

func (s *SlogContextAdapter) LogMode(level logger.LogLevel) logger.Interface {
	// NOTE: We rely on the log level configured on slog.
	return s
}

func (s *SlogContextAdapter) Info(ctx context.Context, msg string, args ...any) {
	s.getLogger(ctx).InfoContext(ctx, fmt.Sprintf(msg, args...))
}

func (s *SlogContextAdapter) Warn(ctx context.Context, msg string, args ...any) {
	s.getLogger(ctx).WarnContext(ctx, fmt.Sprintf(msg, args...))
}

func (s *SlogContextAdapter) Error(ctx context.Context, msg string, args ...any) {
	s.getLogger(ctx).ErrorContext(ctx, fmt.Sprintf(msg, args...))
}

func (s *SlogContextAdapter) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	// Resolve the logger from the context.
	l := s.getLogger(ctx)

	// Build attributes.
	attrs := []any{
		slog.String("sql", sql),
		slog.Int64("rows", rows),
		slog.Duration("latency", elapsed),
		slog.String("source", utils.FileWithLineNum()),
	}

	// Log based on error/latency.
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		l.ErrorContext(ctx, "gorm error", append(attrs, slog.Any("error", err))...)
	case elapsed > s.SlowThreshold && s.SlowThreshold != 0:
		l.WarnContext(ctx, "gorm slow query", append(attrs, slog.Bool("slow", true))...)
	default:
		l.DebugContext(ctx, "gorm query", attrs...)
	}
}
