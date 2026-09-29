package lease

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
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
	slots        *admission.Semaphore
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
	return &Manager{backend: backend, config: config, ctx: ctx, cancel: cancel, slots: admission.New(config.MaxActive), done: make(chan struct{}), declarations: make(map[Name]*declarationID)}, nil
}

// begin reserves one MaxActive slot. A full manager queues in FIFO order for
// at most admission.Wait(OperationTimeout) and the caller's deadline, then
// returns retryable fault.Overloaded; Close ends waits with fault.Closed.
func (m *Manager) begin(ctx context.Context) error {
	if m == nil || m.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "lease requires an initialized manager and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	closing := m.closing
	m.mu.Unlock()
	if closing {
		return fault.New(fault.Closed, "lease manager is closed")
	}
	if err := m.slots.Acquire(ctx, admission.Wait(m.config.OperationTimeout), m.ctx.Done()); err != nil {
		if errors.Is(err, fault.Closed) {
			return fault.Wrap(fault.Closed, "lease manager is closed", err)
		}
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		m.slots.Release()
		return fault.New(fault.Closed, "lease manager is closed")
	}
	m.active++
	return nil
}
func (m *Manager) end() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active--
	m.slots.Release()
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

// PollInterval is the configured contention poll interval, shared by features
// that wait on this manager's leases (for example coordinated cache fills).
func (m *Manager) PollInterval() time.Duration {
	if m == nil {
		return 0
	}
	return m.config.PollInterval
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
