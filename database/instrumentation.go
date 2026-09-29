package database

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

// QueryEvent describes one completed Exec or Query statement. Statement is the
// SQL text as sent: framework statements contain only identifiers, placeholders
// and declared constants, while raw statements contain whatever literals the
// application wrote. Argument values are never exposed.
//
// Duration runs from sending until Exec returned or the Rows stream closed, so
// a streamed query includes the caller's iteration. PoolWait is connection
// acquisition for pool-level statements and zero inside transactions and
// sessions. Rows counts affected (Exec) or returned (Query) rows, -1 when the
// driver could not report it. Code and SQLState are empty on success.
type QueryEvent struct {
	Role      PoolRole
	Operation string
	Statement string
	Duration  time.Duration
	PoolWait  time.Duration
	Rows      int64
	Code      Code
	SQLState  string
}

// QueryObserver receives completed statements synchronously on the executing
// goroutine. It must be fast, must not use the database and must not retain
// the context. A panic is contained and never changes the statement result.
type QueryObserver func(context.Context, QueryEvent)

// WithQueryObserver installs one statement observer for metrics or tracing.
// Observers are fixed for the pool's lifetime.
func WithQueryObserver(observer QueryObserver) Option {
	return func(settings *poolSettings) error {
		if observer == nil {
			return fault.New(fault.Invalid, "database query observer cannot be nil")
		}
		if settings.queryObserver != nil {
			return fault.New(fault.Invalid, "database query observer is already configured")
		}
		settings.queryObserver = observer
		return nil
	}
}

// WithSlowQueryLog logs statements taking at least threshold at Warn level
// with role, operation, duration, pool wait, row count, classification and a
// statement fingerprint (FNV-1a of the SQL text) that groups identical
// statements. Neither SQL text nor argument values are logged.
func WithSlowQueryLog(logger *slog.Logger, threshold time.Duration) Option {
	return func(settings *poolSettings) error {
		if logger == nil || threshold <= 0 {
			return fault.New(fault.Invalid, "slow query logging requires a logger and a positive threshold")
		}
		settings.slowLogger, settings.slowThreshold = logger, threshold
		return nil
	}
}

// WithSlowQueryThreshold enables slow-statement logging through the owning
// application's logger, which a database Module binds during boot. Direct
// Open/Prepare callers supply a logger with WithSlowQueryLog instead; Start
// rejects a threshold that has no logger.
func WithSlowQueryThreshold(threshold time.Duration) Option {
	return func(settings *poolSettings) error {
		if threshold <= 0 {
			return fault.New(fault.Invalid, "slow query threshold must be positive")
		}
		settings.slowThreshold = threshold
		return nil
	}
}

// instrumentation is fixed before Start; nil disables all timing work.
type instrumentation struct {
	observer  QueryObserver
	logger    *slog.Logger
	threshold time.Duration
}

func newInstrumentation(settings poolSettings) *instrumentation {
	if settings.queryObserver == nil && settings.slowThreshold <= 0 {
		return nil
	}
	return &instrumentation{observer: settings.queryObserver, logger: settings.slowLogger, threshold: settings.slowThreshold}
}

// bindRuntimeLogger supplies a Module's application logger to a configured
// slow-statement threshold before Start; an explicit logger is retained.
func (db *DB) bindRuntimeLogger(logger *slog.Logger) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closing || db.startAttempted {
		return fault.New(fault.Closed, "database logger binding is frozen")
	}
	if db.instrument != nil && db.instrument.threshold > 0 && db.instrument.logger == nil {
		db.instrument.logger = logger
	}
	return nil
}

func (i *instrumentation) validate() error {
	if i != nil && i.threshold > 0 && i.logger == nil {
		return fault.New(fault.Invalid, "slow query threshold requires a logger; use WithSlowQueryLog outside a database Module")
	}
	return nil
}

// statementProbe carries one statement's instrumentation context through the funnel.
// Its zero value records nothing and never reads the clock.
type statementProbe struct {
	hooks *instrumentation
	role  PoolRole
	wait  time.Duration
}

func (db *DB) instrumented(role PoolRole) statementProbe {
	if db == nil {
		return statementProbe{}
	}
	return statementProbe{hooks: db.instrument, role: role}
}

// start samples the clock only when instrumentation is enabled.
func (p statementProbe) start() time.Time {
	if p.hooks == nil {
		return time.Time{}
	}
	return time.Now()
}

func (p statementProbe) finish(ctx context.Context, operation, statement string, started time.Time, rows int64, err error) {
	if p.hooks == nil {
		return
	}
	event := QueryEvent{Role: p.role, Operation: operation, Statement: statement, Duration: time.Since(started), PoolWait: p.wait, Rows: rows}
	var classified *Error
	if errors.As(err, &classified) && classified != nil {
		event.Code, event.SQLState = classified.Code(), classified.SQLState()
	} else if err != nil {
		event.Code = QueryFailed
	}
	if p.hooks.observer != nil {
		if failed := callback.Invoke("database query observer", func() error { p.hooks.observer(ctx, event); return nil }); failed != nil && p.hooks.logger != nil {
			p.hooks.logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelError, "database query observer failed", slog.Any("diagnostic", errordiag.Describe(failed)))
		}
	}
	if p.hooks.logger != nil && p.hooks.threshold > 0 && event.Duration >= p.hooks.threshold {
		fingerprint := fnv.New64a()
		_, _ = fingerprint.Write([]byte(statement))
		p.hooks.logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelWarn, "slow database statement",
			slog.String("role", string(event.Role)), slog.String("operation", operation),
			slog.Duration("duration", event.Duration), slog.Duration("pool_wait", event.PoolWait),
			slog.Int64("rows", rows), slog.String("code", string(event.Code)), slog.String("sqlstate", event.SQLState),
			slog.String("statement_fingerprint", strconv.FormatUint(fingerprint.Sum64(), 16)))
	}
}
