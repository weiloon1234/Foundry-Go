package data

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Config struct {
	Namespace                                                 keyspace.Namespace
	MaxDeclarations, MaxKeyBytes, MaxConcurrent, MaxBatchKeys int
	Timeout                                                   time.Duration
	Limits                                                    Limits
}

func DefaultConfig(ns keyspace.Namespace) Config {
	return Config{ns, 256, 1024, 128, 64, 5 * time.Second, DefaultLimits()}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxDeclarations <= 0 || c.MaxKeyBytes <= 0 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.MaxConcurrent <= 0 || c.MaxBatchKeys <= 0 || c.MaxBatchKeys > MaxBatchKeys || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid Redis data store bounds")
	}
	return c.Limits.Validate()
}

type declarationID struct{ marker byte }
type declarationKey struct {
	name    Name
	version Version
}

// Store borrows its adapter. Key/JSON/adapter callbacks retain an operation slot
// until they actually exit, including after cancellation. No detached retries or
// implicit fallback run. Inputs must stay unchanged until their operation returns.
type Store struct {
	backend      Backend
	config       Config
	slots        *admission.Semaphore
	mu           sync.Mutex
	declarations map[declarationKey]*declarationID
}

func NewStore(backend Backend, c Config) (*Store, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "Redis data store requires an adapter")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: c, slots: admission.New(c.MaxConcurrent), declarations: make(map[declarationKey]*declarationID)}, nil
}
func (s *Store) bind(name Name, version Version, id *declarationID) error {
	if s == nil || s.slots == nil {
		return fault.New(fault.Invalid, "Redis data binding requires a store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := declarationKey{name, version}
	old, exists := s.declarations[key]
	if exists && old != id {
		return fault.New(fault.Duplicate, "Redis data name/version has another declaration")
	}
	if !exists && len(s.declarations) >= s.config.MaxDeclarations {
		return fault.New(fault.Invalid, "Redis data declaration capacity reached")
	}
	s.declarations[key] = id
	return nil
}
func (s *Store) execute(ctx context.Context, fn func(context.Context) error) error {
	if s == nil || s.slots == nil || ctx == nil {
		return fault.New(fault.Invalid, "Redis data operation requires a bound store and context")
	}
	op, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if err := op.Err(); err != nil {
		return err
	}
	// A full store queues in FIFO order for at most admission.Wait(Timeout) and
	// the operation deadline, then reports retryable fault.Overloaded.
	if err := s.slots.Acquire(op, admission.Wait(s.config.Timeout), nil); err != nil {
		return err
	}
	defer s.slots.Release()
	err := callback.Isolated("Redis data operation", func() error {
		if err := op.Err(); err != nil {
			return err
		}
		return fn(op)
	})
	if err != nil {
		return err
	}
	return op.Err()
}
