package idempotency

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

// Store borrows an existing primary database and owns bounded operation lifetimes.
// New performs no I/O and starts no background task, migration or connection.
// Start (called by Module) begins the optional expiry pruner; Close stops it.
type Store struct {
	db        *database.DB
	namespace Namespace
	config    Config
	calls     *workscope.Group
	logger    *slog.Logger
	lifecycle *pruning
}

// pruning owns the store's background pruner. exited is closed immediately
// when no pruner runs; done closes after operations and the pruner have exited.
type pruning struct {
	mu      sync.Mutex
	started bool
	closing bool
	stop    chan struct{}
	exited  chan struct{}
	done    chan struct{}
}

// Option configures a Store at construction.
type Option func(*Store) error

// WithLogger borrows a logger for redacted background-pruning failures until
// Done closes. Module uses the application's configured logger by default.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Store) error {
		if logger == nil {
			return invalid("idempotency store requires a non-nil logger")
		}
		s.logger = logger
		return nil
	}
}

func New(db *database.DB, namespace Namespace, config Config, options ...Option) (*Store, error) {
	if db == nil || namespace.Validate() != nil {
		return nil, invalid("idempotency requires a database and application namespace")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	exited := make(chan struct{})
	close(exited)
	store := &Store{db: db, namespace: namespace, config: config, calls: calls, lifecycle: &pruning{stop: make(chan struct{}), exited: exited, done: make(chan struct{})}}
	for _, option := range options {
		if option == nil {
			return nil, invalid("idempotency store option is nil")
		}
		if err := option(store); err != nil {
			return nil, err
		}
	}
	return store, nil
}
func (s *Store) Validate() error {
	if s == nil || s.db == nil || s.calls == nil || s.lifecycle == nil {
		return invalid("idempotency store is undefined")
	}
	return nil
}
func (s *Store) Database() *database.DB {
	if s == nil {
		return nil
	}
	return s.db
}
func (s *Store) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.config
}

// Start begins automatic pruning when Config.PruneInterval is positive. It
// performs no I/O; the first sweep runs one interval later. Repeated calls are
// harmless. Close stops the pruner and waits for any running sweep to exit.
func (s *Store) Start(ctx context.Context) error {
	return s.start(ctx, nil)
}

// start uses fallback only when no logger was configured explicitly.
func (s *Store) start(ctx context.Context, fallback *slog.Logger) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return invalid("idempotency start requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p := s.lifecycle
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return fault.New(fault.Closed, "idempotency store is closed")
	}
	if p.started || s.config.PruneInterval == 0 {
		return nil
	}
	logger := s.logger
	if logger == nil {
		logger = fallback
	}
	p.started = true
	p.exited = make(chan struct{})
	go s.prune(logger, p.stop, p.exited)
	return nil
}

// Close rejects new operations, cancels active ones and stops the pruner. A
// canceled caller stops waiting; owned callbacks and cleanup continue.
func (s *Store) Close(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := s.calls.CheckClose(ctx); err != nil {
		return err
	}
	p := s.lifecycle
	p.mu.Lock()
	if !p.closing {
		p.closing = true
		close(p.stop)
		exited := p.exited
		go func() {
			<-s.calls.Done()
			<-exited
			close(p.done)
		}()
	}
	p.mu.Unlock()
	if err := s.calls.Close(ctx); err != nil {
		return err
	}
	select {
	case <-p.done:
		return nil
	default:
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done closes after Close, once operations and the pruner have exited.
func (s *Store) Done() <-chan struct{} {
	if s == nil || s.lifecycle == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.lifecycle.done
}
func (*Store) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotency store")) }

// The runner owns its outer transaction, so its infrastructure statements set
// transaction-local settings directly instead of joining a savepoint scope
// (internal/sqlscope). Any failure rolls back the whole transaction, including
// these settings; on success they end at commit.
//
// enterStatement captures the caller's search_path and lock_timeout before the
// materialized CTE row is projected, then bounds statement work, bounds the
// claim's duplicate wait and selects the store schema in one round trip.
const enterStatement = `WITH previous AS MATERIALIZED (
 SELECT pg_catalog.current_setting('search_path') AS search_path, pg_catalog.current_setting('lock_timeout') AS lock_timeout
) SELECT previous.search_path, previous.lock_timeout,
 pg_catalog.set_config('statement_timeout', $1, true),
 pg_catalog.set_config('lock_timeout', $2, true),
 pg_catalog.set_config('search_path', $3, true)
FROM previous`

// restoreStatement returns the transaction to the caller's settings before
// application code runs, so only the claim observes DuplicateWait.
const restoreStatement = `SELECT pg_catalog.set_config('search_path', $1, true), pg_catalog.set_config('lock_timeout', $2, true)`

// schemaStatement selects the store schema for a statement that is the last
// work in its transaction.
const schemaStatement = `SELECT pg_catalog.set_config('search_path', $1, true)`

// callerSettings are the transaction settings to restore before application code.
type callerSettings struct{ searchPath, lockTimeout string }

func (s *Store) searchPath() string { return `"` + s.config.Schema + `", pg_temp` }

// enter prepares a runner-owned transaction for the claim.
func (s *Store) enter(ctx context.Context, tx *database.Tx) (callerSettings, error) {
	var previous callerSettings
	var statement, lock, path string
	err := database.ScanOne(ctx, tx, enterStatement, []any{strconv.FormatInt(s.config.Timeout.Milliseconds(), 10), strconv.FormatInt(s.config.DuplicateWait.Milliseconds(), 10), s.searchPath()}, &previous.searchPath, &previous.lockTimeout, &statement, &lock, &path)
	if err != nil {
		return callerSettings{}, err
	}
	if len(previous.searchPath) > 8192 || len(previous.lockTimeout) > 64 {
		return callerSettings{}, invalid("transaction settings exceed the idempotency boundary")
	}
	return previous, nil
}

// leave returns the transaction to the caller's settings.
func (s *Store) leave(ctx context.Context, tx *database.Tx, previous callerSettings) error {
	_, err := tx.Exec(ctx, restoreStatement, previous.searchPath, previous.lockTimeout)
	return err
}

// selectSchema scopes the transaction's final infrastructure statement.
func (s *Store) selectSchema(ctx context.Context, tx *database.Tx) error {
	_, err := tx.Exec(ctx, schemaStatement, s.searchPath())
	return err
}

func (s *Store) namespaceDigest() string {
	return digest("foundry.idempotency.namespace.v1", s.namespace.Application, s.namespace.Environment)
}
