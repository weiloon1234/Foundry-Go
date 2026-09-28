package lease

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Manager owns bounded acquisitions, guards and callbacks, borrowing its Backend.
// Construction is pure. Close cancels work and waits for actual cleanup; it never
// closes the borrowed adapter. Use Module to order cleanup before the adapter.
type Manager struct {
	backend      Backend
	config       Config
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	declarations map[Name]*declarationID
	active       int
	closing      bool
	done         chan struct{}
	closeErr     error
}

func NewManager(backend Backend, config Config) (*Manager, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "lease manager requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{backend: backend, config: config, ctx: ctx, cancel: cancel, done: make(chan struct{}), declarations: make(map[Name]*declarationID)}, nil
}
func (m *Manager) begin(ctx context.Context) error {
	if m == nil || m.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "lease requires an initialized manager and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return fault.New(fault.Closed, "lease manager is closed")
	}
	if m.active >= m.config.MaxActive {
		return fault.New(fault.Conflict, "lease capacity reached")
	}
	m.active++
	return nil
}
func (m *Manager) end() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active--
	if m.closing && m.active == 0 {
		close(m.done)
	}
}
func (m *Manager) cleanupError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeErr == nil {
		m.closeErr = err
	}
}

// Close starts cancellation once. A canceled caller stops waiting, while owned
// cleanup continues. Noncooperative domain callbacks delay Done; they are not abandoned.
// Do not call Close synchronously from one of this manager's own callbacks.
func (m *Manager) Close(ctx context.Context) error {
	if m == nil || m.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "lease close requires an initialized manager and context")
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
		return m.closeErr
	default:
	}
	select {
	case <-m.done:
		return m.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Done() <-chan struct{} {
	if m == nil {
		return nil
	}
	return m.done
}

// Stats reports all reserved slots, including waiters and callbacks still exiting.
type Stats struct {
	Active  int
	Closing bool
}

func (m *Manager) Stats() Stats {
	if m == nil || m.done == nil {
		return Stats{Closing: true}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{Active: m.active, Closing: m.closing}
}

// Namespace identifies the authority scope shared by feature integrations.
func (m *Manager) Namespace() keyspace.Namespace {
	if m == nil {
		return keyspace.Namespace{}
	}
	return m.config.Namespace
}

// BorrowedBackend lets another framework feature use the SAME authority. It does
// not transfer adapter ownership; close dependent features before that adapter.
func (m *Manager) BorrowedBackend() Backend {
	if m == nil {
		return nil
	}
	return m.backend
}

// ValidateScope checks construction-time duration/wait limits without reserving
// capacity or performing I/O. Operations also check lifecycle and cancellation.
func (m *Manager) ValidateScope(ttl, wait time.Duration) error {
	if m == nil || m.done == nil {
		return fault.New(fault.Invalid, "lease manager is not initialized")
	}
	if err := ValidateDuration(ttl); err != nil {
		return err
	}
	if wait < 0 || wait > m.config.MaxWait {
		return fault.New(fault.Invalid, "lease wait exceeds its bound")
	}
	return nil
}
