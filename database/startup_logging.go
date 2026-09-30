package database

import (
	"context"
	"log/slog"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// WithStartupLog supplies diagnostics for direct Open/Prepare callers. Modules
// inherit their application's logger by default. Logs contain pool role,
// attempt, elapsed time, retry delay and classification, never connection
// strings or driver error text. This option does not enable query timing.
func WithStartupLog(logger *slog.Logger) Option {
	return func(settings *poolSettings) error {
		if logger == nil || settings.startupLogger != nil {
			return fault.New(fault.Invalid, "database startup logging requires one non-nil logger")
		}
		settings.startupLogger = logger
		return nil
	}
}

func startupLog(ctx context.Context, logger *slog.Logger, level slog.Level, message string, role PoolRole, attempt int, started time.Time, wait time.Duration, err error) {
	if logger == nil {
		return
	}
	// A custom log handler must not prevent Start from releasing its ownership
	// by panicking or calling Goexit. Wait for actual handler exit; no detached
	// logging work or recursive fallback logger is created.
	_ = callback.Isolated("database startup logger", func() error {
		attributes := []slog.Attr{
			slog.String("role", string(role)), slog.Int("attempt", attempt),
			slog.Duration("elapsed", time.Since(started)), slog.Duration("retry_delay", wait),
		}
		if classified, ok := err.(*Error); ok && classified != nil {
			attributes = append(attributes, slog.String("code", string(classified.Code())), slog.String("sqlstate", classified.SQLState()))
		}
		logger.LogAttrs(context.WithoutCancel(ctx), level, message, attributes...)
		return nil
	})
}
