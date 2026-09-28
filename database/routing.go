package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
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
	conn, release, classify, err := db.acquirePool(ctx, ReadPool)
	if err != nil {
		return nil, err
	}
	return query(ctx, conn, classify, db.Observers(), observerOwner{db: db, ctx: ctx}, release, statement, arguments)
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

// HasReadPool reports configuration, not current endpoint health.
func (db *DB) HasReadPool() bool { return db.read != nil }

// PoolStats describes one endpoint. Resource owners belong to the shared DB and
// appear once in Stats; per-endpoint connection counts are independent.
type PoolStats struct {
	Role                       PoolRole
	MaxOpen, Open, InUse, Idle int
	WaitCount                  int64
	WaitDuration               time.Duration
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

func (db *DB) PingPrimary(ctx context.Context) error { return db.pingPool(ctx, PrimaryPool) }

// PingRead checks the selected read endpoint, falling back only when absent.
func (db *DB) PingRead(ctx context.Context) error { return db.pingPool(ctx, ReadPool) }
func (db *DB) pingPool(ctx context.Context, role PoolRole) (err error) {
	conn, release, classify, err := db.acquirePool(ctx, role)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	return classify.wrap("ping "+string(role), conn.PingContext(ctx))
}

func openPool(adapter Adapter, config PoolConfig) *sql.DB {
	raw := sql.OpenDB(adapter.Connector)
	raw.SetMaxOpenConns(config.MaxOpen)
	raw.SetMaxIdleConns(config.MaxIdle)
	raw.SetConnMaxLifetime(config.MaxLifetime)
	raw.SetConnMaxIdleTime(config.MaxIdleTime)
	return raw
}

func pingStartingPool(ctx context.Context, role PoolRole, raw *sql.DB, config PoolConfig, classify classifier) error {
	connect, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
	defer cancel()
	return classify.wrap("start "+string(role), raw.PingContext(connect))
}

func (db *DB) closePools() error {
	var result error
	if db.raw != nil {
		result = db.classify.wrap("close primary", db.raw.Close())
	}
	if db.read != nil && db.read.raw != nil {
		result = errors.Join(result, classifier(db.read.adapter.Classify).wrap("close read", db.read.raw.Close()))
	}
	return result
}
