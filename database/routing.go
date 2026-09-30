package database

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PoolRole identifies an explicitly selected endpoint, never inferred SQL text.
type PoolRole string

const (
	PrimaryPool PoolRole = "primary"
	ReadPool    PoolRole = "read"
)

type readPoolSettings struct {
	adapter        func() (Adapter, error)
	config         PoolConfig
	maxConnections int
}

type readPool struct {
	adapter Adapter
	config  PoolConfig
	raw     *sql.DB
	probe   *sql.DB
}

// WithReadPool configures an optional read endpoint and an explicit combined
// connection ceiling. The primary and read MaxOpen sum must fit that ceiling.
// The adapter factory runs during Prepare, must be pure and must return a fresh
// or safely shared connector. A configured endpoint never silently falls back
// when unavailable. Without this option reads use the primary pool.
func WithReadPool(adapter func() (Adapter, error), config PoolConfig, maxConnections int) Option {
	return func(settings *poolSettings) error {
		if settings.read != nil || adapter == nil || maxConnections <= 0 {
			return fault.New(fault.Invalid, "read pool requires one adapter factory and a positive combined connection bound")
		}
		if err := config.Validate(); err != nil {
			return err
		}
		settings.read = &readPoolSettings{adapter: adapter, config: config, maxConnections: maxConnections}
		return nil
	}
}

// WithConnectionLimit caps all configured endpoints, including a read pool
// contributed by another option. It does not increase individual pool limits.
// Without this option a single pool's ceiling is its MaxOpen; WithReadPool must
// always supply a combined bound. Repeated limits are rejected.
func WithConnectionLimit(maxConnections int) Option {
	return func(settings *poolSettings) error {
		if maxConnections <= 0 || settings.connectionLimit != 0 {
			return fault.New(fault.Invalid, "database needs one positive combined connection limit")
		}
		settings.connectionLimit = maxConnections
		return nil
	}
}

func prepareReadPool(primary PoolConfig, options poolSettings) (*readPool, int, error) {
	settings := options.read
	limit := options.connectionLimit
	if settings == nil {
		if limit == 0 {
			limit = primary.MaxOpen
		}
		if primary.MaxOpen > limit {
			return nil, 0, fault.New(fault.Invalid, "primary pool exceeds the combined connection bound")
		}
		return nil, limit, nil
	}
	if limit == 0 {
		limit = settings.maxConnections
	} else {
		limit = min(limit, settings.maxConnections)
	}
	if settings.config.MaxOpen > limit || primary.MaxOpen > limit-settings.config.MaxOpen {
		return nil, 0, fault.New(fault.Invalid, "primary and read pool limits exceed the combined connection bound")
	}
	var adapter Adapter
	err := callback.Isolated("construct database read adapter", func() error {
		var err error
		adapter, err = settings.adapter()
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	if adapter.Connector == nil {
		return nil, 0, fault.New(fault.Invalid, "database read adapter needs a connector")
	}
	return &readPool{adapter: adapter, config: settings.config}, limit, nil
}

// ReadRouter is an optional executor capability for explicitly declared reads.
// Wrappers that intend to retain routing should forward QueryRead as well as
// Query; ordinary Executor wrappers deliberately retain their own Query policy.
// Transactions and connection-scoped sessions never implement this capability.
type ReadRouter interface {
	Executor
	QueryRead(context.Context, string, ...any) (*Rows, error)
}

// ReadQuery executes SQL already classified as a read by its caller. Generated
// SELECTs use this boundary; raw Query and writes keep their primary semantics.
// It never examines SQL text and never replaces an actual transaction executor.
func ReadQuery(ctx context.Context, executor Executor, statement string, arguments ...any) (*Rows, error) {
	if router, ok := executor.(ReadRouter); ok {
		return router.QueryRead(ctx, statement, arguments...)
	}
	return executor.Query(ctx, statement, arguments...)
}

// QueryRead uses the configured read pool, or primary when none is configured.
// SQL must be safe to execute on that endpoint. Replica lag means a recent
// primary commit is not necessarily visible; use Primary or the owning Tx.
func (db *DB) QueryRead(ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	role := ReadPool
	if db.read == nil || db.readsStickToPrimary(ctx) {
		role = PrimaryPool
	}
	instrument := db.instrumented(role)
	acquired := instrument.start()
	conn, release, classify, err := db.acquirePool(ctx, role)
	if err != nil {
		return nil, err
	}
	instrument.wait = sinceStart(acquired)
	return query(ctx, conn, classify, instrument, db.Observers(), observerOwner{db: db, ctx: ctx}, release, statement, arguments)
}

// PrimaryExecutor pins all query and transaction work to the primary endpoint.
// It shares lifecycle, observers and shutdown ownership with its original DB.
// Its zero value rejects execution.
type PrimaryExecutor struct{ db *DB }

func (db *DB) Primary() PrimaryExecutor { return PrimaryExecutor{db: db} }
func (p PrimaryExecutor) Exec(ctx context.Context, statement string, arguments ...any) (Result, error) {
	if p.db == nil {
		return Result{}, failure("execute", NotReady)
	}
	return p.db.Exec(ctx, statement, arguments...)
}
func (p PrimaryExecutor) Query(ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	if p.db == nil {
		return nil, failure("query", NotReady)
	}
	return p.db.Query(ctx, statement, arguments...)
}
func (p PrimaryExecutor) Transaction(ctx context.Context, fn func(*Tx) error, options ...TxOptions) error {
	if p.db == nil {
		return failure("transaction", NotReady)
	}
	return p.db.Transaction(ctx, fn, options...)
}

// Clock returns the original DB's application clock, or nil for the zero value.
func (p PrimaryExecutor) Clock() clock.Clock {
	if p.db == nil {
		return nil
	}
	return p.db.Clock()
}

// FoundryAutocommitQuery keeps framework single-statement writes on primary.
func (p PrimaryExecutor) FoundryAutocommitQuery(seal sqlowner.Seal, ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	if p.db == nil {
		return nil, &Error{operation: "query", detail: Detail{Code: NotReady}, outcome: NoCommit}
	}
	return p.db.FoundryAutocommitQuery(seal, ctx, statement, arguments...)
}

// HasReadPool reports configuration, not current endpoint health.
func (db *DB) HasReadPool() bool { return db.read != nil }

// PoolStats describes one endpoint. Resource owners belong to the shared DB and
// appear once in Stats; per-endpoint connection counts are independent. Counts
// and durations are cumulative since Start, suitable for metric counters. The
// dedicated readiness probe connection is not included.
type PoolStats struct {
	Role                       PoolRole
	MaxOpen, Open, InUse, Idle int
	WaitCount                  int64
	WaitDuration               time.Duration
	MaxIdleClosed              int64
	MaxIdleTimeClosed          int64
	MaxLifetimeClosed          int64
}

type RoutingStats struct {
	Primary        PoolStats
	Read           value.Optional[PoolStats]
	MaxConnections int
}

func poolStats(role PoolRole, config PoolConfig, raw *sql.DB) PoolStats {
	result := PoolStats{Role: role, MaxOpen: config.MaxOpen}
	if raw != nil {
		s := raw.Stats()
		result.Open, result.InUse, result.Idle = s.OpenConnections, s.InUse, s.Idle
		result.WaitCount, result.WaitDuration = s.WaitCount, s.WaitDuration
		result.MaxIdleClosed, result.MaxIdleTimeClosed, result.MaxLifetimeClosed = s.MaxIdleClosed, s.MaxIdleTimeClosed, s.MaxLifetimeClosed
	}
	return result
}

func (db *DB) RoutingStats() RoutingStats {
	db.mu.Lock()
	defer db.mu.Unlock()
	result := RoutingStats{Primary: poolStats(PrimaryPool, db.config, db.raw), MaxConnections: db.maxConnections}
	if db.read != nil {
		result.Read = value.Set(poolStats(ReadPool, db.read.config, db.read.raw))
	}
	return result
}

// PoolHealth retains the endpoint identity and a safely formatted database
// error. Unwrapped driver causes remain explicit diagnostic access only.
type PoolHealth struct {
	Role  PoolRole
	Error error
}

func (db *DB) Health(ctx context.Context) []PoolHealth {
	result := []PoolHealth{{Role: PrimaryPool, Error: db.PingPrimary(ctx)}}
	if db.read != nil {
		result = append(result, PoolHealth{Role: ReadPool, Error: db.PingRead(ctx)})
	}
	return result
}

// PingPrimary checks the primary endpoint through its dedicated probe
// connection, so a saturated application pool cannot make readiness fail.
func (db *DB) PingPrimary(ctx context.Context) error { return db.pingPool(ctx, PrimaryPool) }

// PingRead checks the selected read endpoint, falling back only when absent.
func (db *DB) PingRead(ctx context.Context) error { return db.pingPool(ctx, ReadPool) }

// pingPool never queues behind application work: each endpoint owns one probe
// connection outside MaxOpen, opened on demand and retained while idle. The
// pool owner is retained so shutdown still drains an in-flight probe.
func (db *DB) pingPool(ctx context.Context, role PoolRole) error {
	if err := db.own(); err != nil {
		return err
	}
	defer db.release()
	probe, classify := db.probe, db.classify
	if role == ReadPool && db.read != nil {
		probe, classify = db.read.probe, classifier(db.read.adapter.Classify)
	}
	return classify.wrap("ping "+string(role), probe.PingContext(ctx))
}

func openPool(adapter Adapter, config PoolConfig) *sql.DB {
	raw := sql.OpenDB(adapter.Connector)
	raw.SetMaxOpenConns(config.MaxOpen)
	raw.SetMaxIdleConns(config.MaxIdle)
	raw.SetConnMaxLifetime(config.MaxLifetime)
	raw.SetConnMaxIdleTime(config.MaxIdleTime)
	return raw
}

// openProbe prepares, without connecting, one serialized health connection.
func openProbe(adapter Adapter, config PoolConfig) *sql.DB {
	raw := sql.OpenDB(adapter.Connector)
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	raw.SetConnMaxLifetime(config.MaxLifetime)
	raw.SetConnMaxIdleTime(config.MaxIdleTime)
	return raw
}

const (
	startRetryInitial = 100 * time.Millisecond
	startRetryMaximum = 2 * time.Second
)

// pingStartingPool retries transient connection failures within StartupTimeout
// using jittered exponential backoff. Each attempt is bounded by ConnectTimeout
// and the caller's context; authentication failures fail immediately.
func pingStartingPool(ctx context.Context, role PoolRole, raw *sql.DB, config PoolConfig, classify classifier, logger *slog.Logger) error {
	started := time.Now()
	deadline := started.Add(config.StartupTimeout)
	startupLog(ctx, logger, slog.LevelInfo, "database startup started", role, 0, started, 0, nil)
	delay := startRetryInitial
	for attempt := 1; ; attempt++ {
		startupLog(ctx, logger, slog.LevelDebug, "database startup attempt", role, attempt, started, 0, nil)
		connect, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
		err := classify.wrap("start "+string(role), raw.PingContext(connect))
		cancel()
		if err == nil {
			startupLog(ctx, logger, slog.LevelInfo, "database startup ready", role, attempt, started, 0, nil)
			return nil
		}
		if ctx.Err() != nil || !transientStartFailure(err) {
			startupLog(ctx, logger, slog.LevelError, "database startup failed", role, attempt, started, 0, err)
			return err
		}
		wait := delay/2 + rand.N(delay/2+1)
		if time.Now().Add(wait).After(deadline) {
			startupLog(ctx, logger, slog.LevelError, "database startup failed", role, attempt, started, 0, err)
			return err
		}
		startupLog(ctx, logger, slog.LevelWarn, "database startup retry", role, attempt, started, wait, err)
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			canceled := classify.wrap("start "+string(role), ctx.Err())
			startupLog(ctx, logger, slog.LevelError, "database startup canceled", role, attempt, started, 0, canceled)
			return errors.Join(err, canceled)
		}
		delay = min(delay*2, startRetryMaximum)
	}
}

// SQLSTATE class 28 is the standard invalid-authorization class.
func transientStartFailure(err error) bool {
	classified, ok := err.(*Error)
	if !ok || classified == nil {
		return false
	}
	if strings.HasPrefix(classified.detail.SQLState, "28") {
		return false
	}
	return classified.detail.Code == Unavailable || classified.detail.Code == DeadlineExceeded
}

func (db *DB) closePools() error {
	var result error
	if db.raw != nil {
		result = db.classify.wrap("close primary", db.raw.Close())
	}
	if db.probe != nil {
		result = errors.Join(result, db.classify.wrap("close primary probe", db.probe.Close()))
	}
	if db.read != nil && db.read.raw != nil {
		result = errors.Join(result, classifier(db.read.adapter.Classify).wrap("close read", db.read.raw.Close()))
	}
	if db.read != nil && db.read.probe != nil {
		result = errors.Join(result, classifier(db.read.adapter.Classify).wrap("close read probe", db.read.probe.Close()))
	}
	return result
}
