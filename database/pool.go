package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Adapter supplies an explicitly constructed standard Go connector and error
// classification. Applications normally use a feature adapter such as postgres.
// Connector methods must honor their contexts and database/sql's driver contract,
// including error methods invoked inside database/sql. Drivers must not panic or
// call runtime.Goexit. Classify must be safe to share; Foundry owns its invocation.
type Adapter struct {
	Connector driver.Connector
	Classify  func(error) Detail
}

// PoolConfig bounds connection ownership. Use DefaultPoolConfig as a starting
// point, then change fields explicitly. Zero idle/lifetime settings disable idle
// retention/expiry respectively; maximum open connections and timeouts must be
// positive. Query execution uses its caller's context, not AcquireTimeout.
type PoolConfig struct {
	MaxOpen        int
	MaxIdle        int
	MaxLifetime    time.Duration
	MaxIdleTime    time.Duration
	ConnectTimeout time.Duration
	AcquireTimeout time.Duration
}

// DefaultPoolConfig returns a fresh, bounded configuration for one process.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{MaxOpen: 16, MaxIdle: 2, MaxLifetime: 30 * time.Minute, MaxIdleTime: 5 * time.Minute, ConnectTimeout: 5 * time.Second, AcquireTimeout: 5 * time.Second}
}

// Validate rejects impossible or unbounded connection/acquisition settings.
func (c PoolConfig) Validate() error {
	if c.MaxOpen <= 0 || c.MaxIdle < 0 || c.MaxIdle > c.MaxOpen || c.MaxLifetime < 0 || c.MaxIdleTime < 0 || c.ConnectTimeout <= 0 || c.AcquireTimeout <= 0 {
		return fault.New(fault.Invalid, "invalid database pool bounds")
	}
	return nil
}

// DB is a concurrency-safe, application-owned connection pool. Close rejects new
// work and drains active resource owners. Rows and transactions must be closed.
type DB struct {
	raw              *sql.DB
	read             *readPool
	maxConnections   int
	adapter          Adapter
	config           PoolConfig
	classify         classifier
	mu               sync.Mutex
	closing          bool
	ready            bool
	startAttempted   bool
	startDone        chan struct{}
	startErr         error
	active           int
	drained          chan struct{}
	done             chan struct{}
	closeErr         error
	observers        lifecycle.Observers
	observersBound   bool
	managedObservers bool
	timeSource       clock.Clock
	clockExplicit    bool
	clockBound       bool
}

// Open constructs a pool and verifies connectivity before returning it. The
// adapter is an infrastructure boundary; credentials are supplied by its caller.
// Failure closes the newly owned pool. No schemas or migrations are applied.
func Open(ctx context.Context, adapter Adapter, config PoolConfig, options ...Option) (*DB, error) {
	db, err := Prepare(adapter, config, options...)
	if err != nil {
		return nil, err
	}
	if err := db.Start(ctx); err != nil {
		return nil, errors.Join(err, db.Close(context.Background()))
	}
	return db, nil
}

// Prepare validates a pool without opening connections or starting goroutines.
// Start must succeed before operations can acquire connections. This supports
// pure provider registration and constructor injection before lifecycle boot.
func Prepare(adapter Adapter, config PoolConfig, options ...Option) (*DB, error) {
	if adapter.Connector == nil {
		return nil, fault.New(fault.Invalid, "database adapter needs a connector")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	settings, err := configurePool(options)
	if err != nil {
		return nil, err
	}
	read, maxConnections, err := prepareReadPool(config, settings)
	if err != nil {
		return nil, err
	}
	return &DB{read: read, maxConnections: maxConnections, timeSource: settings.clock, clockExplicit: settings.clockSet, clockBound: true, adapter: adapter, config: config, classify: adapter.Classify, startDone: make(chan struct{}), drained: make(chan struct{}), done: make(chan struct{})}, nil
}

// Start verifies a prepared pool once. The first caller's context controls that
// attempt; concurrent callers may stop waiting using their own contexts. A failed
// start is terminal for this pool; prepare another instance for a deliberate retry.
func (db *DB) Start(ctx context.Context) error {
	db.mu.Lock()
	if db.closing {
		db.mu.Unlock()
		return failure("start", Closed)
	}
	if db.startAttempted {
		db.mu.Unlock()
		select {
		case <-db.startDone:
			return db.startErr
		default:
		}
		select {
		case <-db.startDone:
			return db.startErr
		case <-ctx.Done():
			return db.classify.wrap("start", ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		db.mu.Unlock()
		return db.classify.wrap("start", err)
	}
	if db.managedObservers && !db.observersBound {
		db.mu.Unlock()
		return failure("start before observer binding", NotReady)
	}
	if !db.clockBound {
		db.mu.Unlock()
		return failure("start before application clock binding", NotReady)
	}
	db.observersBound = true
	db.startAttempted = true
	db.active++ // Close must retain ownership while connectivity is being checked.
	db.mu.Unlock()
	raw := openPool(db.adapter, db.config)
	var readRaw *sql.DB
	if db.read != nil {
		readRaw = openPool(db.read.adapter, db.read.config)
	}
	db.mu.Lock()
	db.raw = raw
	if db.read != nil {
		db.read.raw = readRaw
	}
	db.mu.Unlock()
	err := pingStartingPool(ctx, PrimaryPool, raw, db.config, db.classify)
	if err == nil && db.read != nil {
		err = pingStartingPool(ctx, ReadPool, db.read.raw, db.read.config, classifier(db.read.adapter.Classify))
	}
	if err != nil {
		err = errors.Join(err, db.closePools())
	}
	db.mu.Lock()
	if err == nil && db.closing {
		err = failure("start", Closed)
	}
	db.ready = err == nil
	db.startErr = err
	close(db.startDone)
	db.mu.Unlock()
	db.release()
	return err
}

// Stats is a snapshot of standard pool utilization and Foundry resource owners.
// Owners includes work waiting to acquire a connection, rows, transaction scopes
// and their connections. After-commit callbacks retain the scope owner only.
type Stats struct {
	Open         int
	InUse        int
	Idle         int
	WaitCount    int64
	WaitDuration time.Duration
	Owners       int
	Closing      bool
	Ready        bool
}

func (db *DB) Stats() Stats {
	db.mu.Lock()
	defer db.mu.Unlock()
	var raw sql.DBStats
	if db.raw != nil {
		raw = db.raw.Stats()
	}
	if db.read != nil && db.read.raw != nil {
		read := db.read.raw.Stats()
		raw.OpenConnections += read.OpenConnections
		raw.InUse += read.InUse
		raw.Idle += read.Idle
		raw.WaitCount += read.WaitCount
		raw.WaitDuration += read.WaitDuration
	}
	return Stats{Open: raw.OpenConnections, InUse: raw.InUse, Idle: raw.Idle, WaitCount: raw.WaitCount, WaitDuration: raw.WaitDuration, Owners: db.active, Closing: db.closing, Ready: db.ready && !db.closing}
}

// Done closes only when all resource owners have released and the pool has
// actually closed. A Close deadline does not imply this channel has closed.
func (db *DB) Done() <-chan struct{} { return db.done }

// Close rejects new work immediately, then waits for resources to drain. It is
// safe to repeat concurrently. A canceled caller stops waiting; cleanup remains
// active. Existing work keeps its own context and is not force-terminated.
func (db *DB) Close(ctx context.Context) error {
	db.mu.Lock()
	if !db.closing {
		db.closing = true
		if db.active == 0 {
			close(db.drained)
		}
		go func() {
			<-db.drained
			db.closeErr = db.closePools()
			close(db.done)
		}()
	}
	db.mu.Unlock()
	select {
	case <-db.done:
		return db.closeErr
	default:
	}
	select {
	case <-db.done:
		return db.closeErr
	case <-ctx.Done():
		return db.classify.wrap("close", ctx.Err())
	}
}

func (db *DB) own() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closing {
		return failure("acquire", Closed)
	}
	if !db.ready {
		return failure("acquire", NotReady)
	}
	db.active++
	return nil
}

func (db *DB) release() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.active--
	if db.closing && db.active == 0 {
		close(db.drained)
	}
}

func (db *DB) acquire(ctx context.Context) (*sql.Conn, func() error, error) {
	conn, release, _, err := db.acquirePool(ctx, PrimaryPool)
	return conn, release, err
}

func (db *DB) acquirePool(ctx context.Context, role PoolRole) (*sql.Conn, func() error, classifier, error) {
	if err := db.own(); err != nil {
		return nil, nil, nil, err
	}
	raw, config, classify := db.raw, db.config, db.classify
	if role == ReadPool && db.read != nil {
		raw, config, classify = db.read.raw, db.read.config, classifier(db.read.adapter.Classify)
	}
	acquire, cancel := context.WithTimeout(ctx, config.AcquireTimeout)
	defer cancel()
	conn, err := raw.Conn(acquire)
	if err != nil {
		defer db.release()
		return nil, nil, classify, classify.wrap("acquire "+string(role), err)
	}
	var release sync.Once
	var closeErr error
	return conn, func() error {
		release.Do(func() {
			err := conn.Close()
			if errors.Is(err, sql.ErrConnDone) {
				err = nil
			}
			closeErr = classify.wrap("release "+string(role), err)
			db.release()
		})
		return closeErr
	}, classify, nil
}

// Ping checks every configured endpoint using the caller's context. Dependency
// failure affects readiness; it is not a process liveness failure.
func (db *DB) Ping(ctx context.Context) (err error) {
	for _, health := range db.Health(ctx) {
		err = errors.Join(err, health.Error)
	}
	return err
}
