package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/encryption"
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
//
// CommitTimeout bounds COMMIT and ROLLBACK after a transaction callback returns.
// Completion is detached from caller cancellation, so a client disconnect
// cannot turn an in-flight COMMIT into an unknown outcome. StartupTimeout is the
// total budget Start may spend retrying transient connection failures such as a
// database that is still starting; zero makes one attempt. Authentication and
// other non-transient failures are never retried.
type PoolConfig struct {
	MaxOpen        int
	MaxIdle        int
	MaxLifetime    time.Duration
	MaxIdleTime    time.Duration
	ConnectTimeout time.Duration
	AcquireTimeout time.Duration
	CommitTimeout  time.Duration
	StartupTimeout time.Duration
}

// DefaultPoolConfig returns a fresh, bounded configuration for one process.
// Idle retention equals MaxOpen so steady load does not churn connections;
// MaxIdleTime still closes connections left unused after a burst.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{MaxOpen: 16, MaxIdle: 16, MaxLifetime: 30 * time.Minute, MaxIdleTime: 5 * time.Minute, ConnectTimeout: 5 * time.Second, AcquireTimeout: 5 * time.Second, CommitTimeout: 30 * time.Second, StartupTimeout: 15 * time.Second}
}

// Validate rejects impossible or unbounded connection/acquisition settings.
func (c PoolConfig) Validate() error {
	if c.MaxOpen <= 0 || c.MaxIdle < 0 || c.MaxIdle > c.MaxOpen || c.MaxLifetime < 0 || c.MaxIdleTime < 0 || c.ConnectTimeout <= 0 || c.AcquireTimeout <= 0 || c.CommitTimeout <= 0 || c.StartupTimeout < 0 {
		return fault.New(fault.Invalid, "invalid database pool bounds")
	}
	return nil
}

// Resource-owner state is one atomic word so ordinary acquisition and release
// never take the pool mutex. Close sets ownerClosing once; whichever of Close or
// the final release observes closing with no owners closes drained exactly once.
const (
	ownerClosing uint64 = 1 << 62
	ownerReady   uint64 = 1 << 61
	ownerCount   uint64 = ownerReady - 1
)

// DB is a concurrency-safe, application-owned connection pool. Close rejects new
// work and drains active resource owners. Rows and transactions must be closed.
type DB struct {
	startupLogger    *slog.Logger
	instrument       *instrumentation
	stickyWindow     time.Duration
	raw              *sql.DB
	probe            *sql.DB
	read             *readPool
	maxConnections   int
	adapter          Adapter
	config           PoolConfig
	classify         classifier
	mu               sync.Mutex
	closing          bool
	startAttempted   bool
	startDone        chan struct{}
	startErr         error
	owners           atomic.Uint64
	drained          chan struct{}
	done             chan struct{}
	closeErr         error
	observers        lifecycle.Observers
	observersBound   bool
	managedObservers bool
	timeSource       clock.Clock
	// fieldKeys encrypts and decrypts encrypted model fields; nil disables them.
	fieldKeys     *encryption.Keyring
	clockExplicit bool
	clockBound    bool
	// frozen publishes observers and clock after Start fixes them for life, so
	// hot query paths read them without taking the pool mutex.
	frozen atomic.Bool
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
	return &DB{startupLogger: settings.startupLogger, instrument: newInstrumentation(settings), stickyWindow: settings.stickyWindow, fieldKeys: settings.encryption, read: read, maxConnections: maxConnections, timeSource: settings.clock, clockExplicit: settings.clockSet, clockBound: true, adapter: adapter, config: config, classify: adapter.Classify, startDone: make(chan struct{}), drained: make(chan struct{}), done: make(chan struct{})}, nil
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
	if err := db.instrument.validate(); err != nil {
		db.mu.Unlock()
		return err
	}
	db.observersBound = true
	db.startAttempted = true
	db.frozen.Store(true)
	db.owners.Add(1) // Close must retain ownership while connectivity is being checked.
	db.mu.Unlock()
	raw, probe := openPool(db.adapter, db.config), openProbe(db.adapter, db.config)
	var readRaw, readProbe *sql.DB
	if db.read != nil {
		readRaw, readProbe = openPool(db.read.adapter, db.read.config), openProbe(db.read.adapter, db.read.config)
	}
	db.mu.Lock()
	db.raw, db.probe = raw, probe
	if db.read != nil {
		db.read.raw, db.read.probe = readRaw, readProbe
	}
	db.mu.Unlock()
	err := pingStartingPool(ctx, PrimaryPool, raw, db.config, db.classify, db.startupLogger)
	if err == nil && db.read != nil {
		err = pingStartingPool(ctx, ReadPool, db.read.raw, db.read.config, classifier(db.read.adapter.Classify), db.startupLogger)
	}
	if err != nil {
		err = errors.Join(err, db.closePools())
	}
	db.mu.Lock()
	if err == nil && db.closing {
		err = failure("start", Closed)
	}
	if err == nil {
		db.owners.Or(ownerReady)
	}
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
	owners := db.owners.Load()
	return Stats{Open: raw.OpenConnections, InUse: raw.InUse, Idle: raw.Idle, WaitCount: raw.WaitCount, WaitDuration: raw.WaitDuration, Owners: int(owners & ownerCount), Closing: owners&ownerClosing != 0, Ready: owners&(ownerReady|ownerClosing) == ownerReady}
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
		if db.owners.Or(ownerClosing)&ownerCount == 0 {
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
	for {
		current := db.owners.Load()
		switch {
		case current&ownerClosing != 0:
			return failure("acquire", Closed)
		case current&ownerReady == 0:
			return failure("acquire", NotReady)
		case current&ownerCount == ownerCount:
			return failure("acquire", Busy)
		}
		if db.owners.CompareAndSwap(current, current+1) {
			return nil
		}
	}
}

func (db *DB) release() {
	if db.owners.Add(^uint64(0))&(ownerClosing|ownerCount) == ownerClosing {
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
		failure := classify.wrap("acquire "+string(role), err)
		if ctx.Err() == nil && acquire.Err() != nil {
			failure = poolExhausted(failure)
		}
		return nil, nil, classify, failure
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
