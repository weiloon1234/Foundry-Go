package extensions

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Config bounds the shared store. MaxActive admits that many concurrent
// operations; a burst beyond it queues in FIFO order for a short bounded wait
// before failing with fault.Overloaded, so latency spikes delay reads instead of
// rejecting them. Timeout bounds each admitted operation.
type Config struct {
	Schema    string
	Clock     clock.Clock
	MaxActive int
	Timeout   time.Duration
}

func DefaultConfig() Config {
	return Config{Schema: "public", Clock: clock.System{}, MaxActive: 64, Timeout: time.Minute}
}
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || c.Clock == nil || c.MaxActive < 1 || c.MaxActive > 1024 || c.Timeout <= 0 || c.Timeout > 10*time.Minute {
		return invalid("invalid model extension store configuration")
	}
	v := reflect.ValueOf(c.Clock)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if v.IsNil() {
			return invalid("invalid model extension clock")
		}
	}
	return nil
}

// Store owns only operation admission. The database and immutable owner registry
// are borrowed. Constructing a store does not connect, migrate or seed data.
type Store struct {
	db       *database.DB
	registry *Registry
	config   Config
	calls    *workscope.Group
}

func New(db *database.DB, registry *Registry, config Config) (*Store, error) {
	if db == nil {
		return nil, invalid("model extensions require a database")
	}
	if err := registry.Validate(); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, registry: registry, config: config, calls: calls}, nil
}
func (s *Store) Validate() error {
	if s == nil || s.db == nil || s.calls == nil {
		return invalid("invalid model extension store")
	}
	return s.registry.Validate()
}
func (s *Store) Registry() *Registry {
	if s == nil {
		return nil
	}
	return s.registry
}
func (s *Store) Database() *database.DB {
	if s == nil {
		return nil
	}
	return s.db
}
func (s *Store) Schema() string {
	if s == nil {
		return ""
	}
	return s.config.Schema
}

// Clock is the store's configured time source. Feature caches use it for
// expiry so tests and applications share one injected clock.
func (s *Store) Clock() clock.Clock {
	if s == nil {
		return nil
	}
	return s.config.Clock
}
func (s *Store) Now() (temporal.DateTime, error) {
	if err := s.Validate(); err != nil {
		return temporal.DateTime{}, err
	}
	now, err := temporal.NewDateTime(s.config.Clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil || now.IsZero() {
		return temporal.DateTime{}, invalid("invalid model extension clock value")
	}
	return now, nil
}
func (s *Store) Close(ctx context.Context) error {
	if s == nil {
		return invalid("invalid model extension store")
	}
	return s.calls.Close(ctx)
}
func (s *Store) Done() <-chan struct{} {
	if s == nil {
		var g *workscope.Group
		return g.Done()
	}
	return s.calls.Done()
}
func (*Store) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("model extension store")) }

// Read holds one read-only repeatable snapshot across owner checks and batched
// extension reads. It never adds row locks or invokes model presentation hooks.
func (s *Store) Read(ctx context.Context, fn func(context.Context, *database.Tx) error) error {
	return s.transaction(ctx, fn, database.TxOptions{Isolation: database.RepeatableRead, ReadOnly: true})
}

// ReadFor reads inside the caller's transaction when executor is a
// transaction of this store's exact pool, so slot loads see that transaction's
// own uncommitted writes; the savepoint restores its search path. Any other
// executor, including custom wrappers, reads from Read's own snapshot.
func (s *Store) ReadFor(ctx context.Context, executor database.Executor, fn func(context.Context, *database.Tx) error) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if tx, ok := executor.(*database.Tx); ok && tx.BelongsTo(s.db) {
		return s.Join(ctx, tx, fn)
	}
	return s.Read(ctx, fn)
}

// Write opens a normal transaction. Extension writers must Lock their owner
// before modifying rows. Unknown commit outcomes must be reconciled, not undone
// by deleting external objects.
func (s *Store) Write(ctx context.Context, fn func(context.Context, *database.Tx) error) error {
	return s.transaction(ctx, fn, database.TxOptions{Isolation: database.ReadCommitted})
}
func (s *Store) transaction(ctx context.Context, fn func(context.Context, *database.Tx) error, options database.TxOptions) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if fn == nil {
		return invalid("model extension transaction requires a callback")
	}
	return s.calls.Run(ctx, "model extension transaction", func(ctx context.Context) error {
		return s.db.Transaction(ctx, func(tx *database.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.config.Schema+`", pg_temp`); err != nil {
				return err
			}
			return fn(ctx, tx)
		}, options)
	})
}

// Join contains infrastructure writes in a savepoint of this store's exact
// borrowed pool. It restores the caller's search_path and never commits the
// outer transaction. Lifecycle adapters use this for atomic owner cleanup.
func (s *Store) Join(ctx context.Context, tx *database.Tx, fn func(context.Context, *database.Tx) error) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if fn == nil {
		return invalid("model extension transaction requires a callback")
	}
	return s.calls.Run(ctx, "join model extension transaction", func(ctx context.Context) error {
		return sqlscope.InSchema(ctx, tx, s.db, s.config.Schema, func(child *database.Tx) error { return fn(ctx, child) })
	})
}
