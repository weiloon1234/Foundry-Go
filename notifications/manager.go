package notifications

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type Config struct {
	Schema    string
	Clock     clock.Clock
	MaxActive int
	Timeout   time.Duration
}

func DefaultConfig() Config {
	return Config{Schema: "public", Clock: clock.System{}, MaxActive: 32, Timeout: time.Minute}
}
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || nilInterface(c.Clock) || c.MaxActive < 1 || c.MaxActive > 1024 || c.Timeout <= 0 || c.Timeout > 10*time.Minute {
		return invalid()
	}
	return nil
}

// Manager borrows every dependency. It owns admission and active call lifetimes;
// Close cancels work and Done closes only after actual callback/resource exit.
// Never copy a Manager. Callbacks must honor cancellation and be concurrency-safe.
type Manager struct {
	db       *database.DB
	registry *Registry
	config   Config
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   int
	closing  bool
	done     chan struct{}
}

func New(db *database.DB, registry *Registry, config Config) (*Manager, error) {
	if db == nil || registry == nil || registry.entries == nil {
		return nil, invalid()
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{db: db, registry: registry, config: config, ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
}

type operationKey struct{}
type operationFrame struct {
	manager *Manager
	parent  *operationFrame
	active  atomic.Bool
}

func (m *Manager) begin(ctx context.Context) (context.Context, func(), error) {
	if m == nil || m.done == nil || ctx == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	if m.closing || m.active >= m.config.MaxActive {
		m.mu.Unlock()
		return nil, nil, fault.New(fault.Conflict, "notification manager is unavailable")
	}
	m.active++
	m.mu.Unlock()
	ctx, unlink := contextlink.Link(ctx, m.ctx)
	ctx, cancel := context.WithTimeout(ctx, m.config.Timeout)
	parent, _ := ctx.Value(operationKey{}).(*operationFrame)
	frame := &operationFrame{manager: m, parent: parent}
	frame.active.Store(true)
	ctx = context.WithValue(ctx, operationKey{}, frame)
	return ctx, func() {
		frame.active.Store(false)
		cancel()
		unlink()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.active--
		if m.closing && m.active == 0 {
			close(m.done)
		}
	}, nil
}
func (m *Manager) Done() <-chan struct{} {
	if m == nil || m.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return m.done
}
func (m *Manager) Close(ctx context.Context) error {
	if m == nil || m.done == nil || ctx == nil {
		return invalid()
	}
	for frame, _ := ctx.Value(operationKey{}).(*operationFrame); frame != nil; frame = frame.parent {
		if frame.manager == m && frame.active.Load() {
			return fault.New(fault.Cycle, "notification callback cannot wait for itself")
		}
	}
	m.mu.Lock()
	if !m.closing {
		m.closing = true
		m.cancel()
		if m.active == 0 {
			close(m.done)
		}
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	default:
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (*Manager) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("notification manager")) }
func (m *Manager) within(ctx context.Context, fn func(*database.Tx) error) error {
	return m.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+m.config.Schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted})
}
func (m *Manager) now() (temporal.DateTime, error) {
	now, err := temporal.NewDateTime(m.config.Clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil || now.IsZero() {
		return temporal.DateTime{}, invalid()
	}
	return now, nil
}
