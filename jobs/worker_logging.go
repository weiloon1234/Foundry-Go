package jobs

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

type WorkerOption func(*Worker) error

// WithWorkerLogger borrows a logger until the worker's Done closes. Module and
// WorkerModule automatically use the application's configured logger. Standalone
// NewWorker callers can opt in without changing any process-global logger.
func WithWorkerLogger(logger *slog.Logger) WorkerOption {
	return func(worker *Worker) error {
		if logger == nil {
			return fault.New(fault.Invalid, "job worker requires a non-nil logger")
		}
		worker.logger = logger
		return nil
	}
}

func (w *Worker) logAttempt(ctx context.Context, envelope Envelope, attempt, retries uint32, result Result, committed bool, diagnostic fault.Diagnostic) {
	if w.logger == nil || !w.config.FailureLog {
		return
	}
	if committed && (result.State == Succeeded || result.Reason == RateLimited || result.Reason == CancelRequested || result.Reason == WorkerStopped && result.State != Failed) {
		return
	}
	level := slog.LevelWarn
	message := "job attempt failed"
	if result.State == Failed || !committed {
		level = slog.LevelError
	}
	if !committed {
		message = "job completion unconfirmed"
	}
	// Only primitive, framework-owned metadata and a redacted diagnostic (type
	// names, framework fault codes, panic frames) reach custom handlers. Logging
	// happens after finalization and never turns a successful effect into a retry.
	// An uncooperative sink still owns its worker slot until it actually exits.
	_ = callback.Isolated("log job attempt", func() error {
		attrs := []slog.Attr{slog.String("job_id", envelope.ID().String()), slog.String("job", string(envelope.Name())),
			slog.Uint64("version", uint64(envelope.Version())), slog.String("queue", string(envelope.Queue())),
			slog.Uint64("attempt", uint64(attempt)), slog.Uint64("max_attempts", uint64(envelope.Policy().Attempts)),
			slog.Uint64("retry", uint64(retries)),
			slog.String("reason", string(result.Reason)), slog.String("requested_state", string(result.State)),
			slog.Duration("retry_delay", result.Delay), slog.Bool("finalized", committed)}
		if !diagnostic.IsZero() {
			attrs = append(attrs, slog.Any("diagnostic", diagnostic))
		}
		w.logger.LogAttrs(ctx, level, message, attrs...)
		return nil
	})
}

// logBackendFailure records a queue-authority failure that the worker survives:
// it backs off and keeps running. The diagnostic never contains error text.
func (w *Worker) logBackendFailure(ctx context.Context, operation string, key Key, err error) {
	if w.logger == nil || !w.config.FailureLog {
		return
	}
	diagnostic := errordiag.Describe(err)
	_ = callback.Isolated("log worker failure", func() error {
		w.logger.LogAttrs(ctx, slog.LevelError, "job backend operation failed",
			slog.String("operation", operation), slog.String("queue", string(key.Queue())),
			slog.Any("diagnostic", diagnostic))
		return nil
	})
}
