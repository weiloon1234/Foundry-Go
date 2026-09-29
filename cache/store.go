package cache

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

// Store binds cache declarations to one namespace and a borrowed Backend.
// NewStore is pure construction: it performs no I/O and starts no goroutines.
// Each application builds its own Store and owns the adapter lifecycle.
// Remember and Flexible run loaders in Store-owned fills; Close stops new
// operations and drains those fills before the owner closes the backend.
// TaggedBackend adapters automatically protect every value/counter operation with
// a reserved namespace generation, enabling Invalidate across stores and processes.
// FlushBackend adapters invalidate a namespace by physically removing its entries.
type Store struct {
	coordination *coordinator
	backend      Backend
	// lifetime cancels running fills when Close begins; closed rejects new
	// operations (checked lock-free); running counts fills still executing.
	lifetime     context.Context
	stop         context.CancelFunc
	closed       atomic.Bool
	running      int
	drained      chan struct{}
	config       Config
	namespaceTag EntryKey
	logger       *slog.Logger
	observer     Observer
	counters     counters
	mu           sync.Mutex
	declarations map[Name]*declarationID
	fills        map[EntryKey]*fill
}

// StoreOption configures optional Store collaborators at construction.
type StoreOption func(*Store) error

// WithLogger borrows a structured logger for cache failures that do not fail the
// caller, such as a Remember whose loaded value could not be published. Records
// carry the cache family and a redacted errordiag diagnostic, never keys, values
// or arbitrary error text. Application assembly supplies its configured logger.
func WithLogger(logger *slog.Logger) StoreOption {
	return func(s *Store) error {
		if logger == nil {
			return fault.New(fault.Invalid, "cache store logger is nil")
		}
		s.logger = logger
		return nil
	}
}

func NewStore(backend Backend, config Config, options ...StoreOption) (*Store, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "cache store requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	namespaceTag, err := NewNamespaceTagKey(config.Namespace)
	if err != nil {
		return nil, err
	}
	lifetime, stop := context.WithCancel(context.Background())
	store := &Store{backend: backend, config: config, namespaceTag: namespaceTag, lifetime: lifetime, stop: stop, drained: make(chan struct{}), declarations: make(map[Name]*declarationID), fills: make(map[EntryKey]*fill)}
	for _, option := range options {
		if option == nil {
			return nil, fault.New(fault.Invalid, "cache store option is nil")
		}
		if err := option(store); err != nil {
			return nil, err
		}
	}
	return store, nil
}
func (s *Store) Namespace() Namespace { return s.config.Namespace }

// Close rejects new operations with fault.Closed, cancels the contexts of
// running fills (Remember loaders whose callers have gone, Flexible background
// refreshes and their publications) and waits for them to exit. ctx bounds only
// the wait: a callback that ignores cancellation keeps running, and Done closes
// when it has actually exited. Close never closes the borrowed backend; call it
// before the backend's owner does. Repeated calls wait again.
func (s *Store) Close(ctx context.Context) error {
	if s == nil || s.drained == nil || ctx == nil {
		return fault.New(fault.Invalid, "cache store close needs an initialized store and context")
	}
	s.mu.Lock()
	if !s.closed.Swap(true) {
		s.stop()
		s.finishLocked()
	}
	s.mu.Unlock()
	select {
	case <-s.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done closes after Close once every running fill has exited.
func (s *Store) Done() <-chan struct{} { return s.drained }

// finishLocked closes drained after Close once no fill runs. Call with s.mu held.
func (s *Store) finishLocked() {
	if s.closed.Load() && s.running == 0 {
		select {
		case <-s.drained:
		default:
			close(s.drained)
		}
	}
}

// active rejects operations on a closed Store.
func (s *Store) active() error {
	if s.closed.Load() {
		return fault.New(fault.Closed, "cache store is closed")
	}
	return nil
}

// counters are lock-free operation totals owned by one Store.
type counters struct {
	hits, misses, writes, writeFailures, loads, coalesced, uncoalesced, snapshotRetries atomic.Uint64
}

// Stats is a snapshot of one Store's typed operation totals since construction.
// Hits and Misses count Get/Remember reads; Writes counts successful Put, Add
// and Remember publications; WriteFailures counts Remember publications that
// failed after a successful load (the caller still received the value).
// Loads counts Remember loader runs; Coalesced counts callers that joined an
// existing fill; Uncoalesced counts loads run outside the fill registry because
// MaxFills was exhausted. SnapshotRetries counts tagged reads that re-resolved
// a snapshot changed by a concurrent invalidation.
type Stats struct {
	Hits, Misses, Writes, WriteFailures uint64
	Loads, Coalesced, Uncoalesced       uint64
	SnapshotRetries                     uint64
}

func (s *Store) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	c := &s.counters
	return Stats{Hits: c.hits.Load(), Misses: c.misses.Load(), Writes: c.writes.Load(), WriteFailures: c.writeFailures.Load(), Loads: c.loads.Load(), Coalesced: c.coalesced.Load(), Uncoalesced: c.uncoalesced.Load(), SnapshotRetries: c.snapshotRetries.Load()}
}

// recordFailure reports a cache failure that did not fail the caller. The
// logger is application-owned, so it runs isolated; it never sees the cause's
// text, only its redacted diagnostic.
func (s *Store) recordFailure(ctx context.Context, message string, family Name, err error) {
	if s.logger == nil || err == nil {
		return
	}
	diagnostic := errordiag.Describe(err)
	_ = callback.Isolated("log cache failure", func() error {
		s.logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelWarn, message, slog.String("cache", string(family)), slog.Any("diagnostic", diagnostic))
		return nil
	})
}
