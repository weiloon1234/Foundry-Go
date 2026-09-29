package raw

import (
	"context"
	"sync"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Backend executes validated immutable requests without automatic retries. Batch
// errors may follow partial execution; Transaction errors do not roll back earlier
// successful commands. Capture and bound every reply, including ignored results.
// ExpireRaw uses one atomic native operation, preserving contents and returning
// true for an existing key even when Forever is already set. The caller owns the
// adapter lifecycle. A lost acknowledgement leaves the outcome unknown.
type Backend interface {
	ExecuteRaw(context.Context, Request, Limits) (Reply, error)
	ExecuteRawBatch(context.Context, []Request, Mode, Limits) ([]Reply, error)
	ExpireRaw(context.Context, Key, cache.TTL) (bool, error)
}
type Store struct {
	backend      Backend
	config       Config
	slots        *admission.Semaphore
	mu           sync.Mutex
	declarations map[declarationKey]*declarationID
}

func NewStore(backend Backend, c Config) (*Store, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "raw Redis store requires a backend")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: c, slots: admission.New(c.MaxConcurrent), declarations: make(map[declarationKey]*declarationID)}, nil
}
func (s *Store) bind(name Name, v Version, id *declarationID) error {
	if s == nil || s.slots == nil {
		return fault.New(fault.Invalid, "raw Redis binding requires a store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := declarationKey{name, v}
	old, exists := s.declarations[key]
	if exists && old != id {
		return fault.New(fault.Duplicate, "raw Redis key name/version already declared")
	}
	if !exists && len(s.declarations) >= s.config.MaxDeclarations {
		return fault.New(fault.Invalid, "raw Redis declaration capacity reached")
	}
	s.declarations[key] = id
	return nil
}
func (s *Store) execute(ctx context.Context, fn func(context.Context) error) error {
	if s == nil || s.slots == nil || ctx == nil {
		return fault.New(fault.Invalid, "raw Redis operation requires a bound store and context")
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
	err := callback.Isolated("raw Redis operation", func() error {
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
func (c Command[R]) run(ctx context.Context, s *Store) (R, error) {
	if c.decoder.decode == nil {
		return *new(R), replyError()
	}
	if err := c.request.Validate(s.config.Namespace, s.config.Limits); err != nil {
		return *new(R), err
	}
	reply, err := s.backend.ExecuteRaw(ctx, c.request, s.config.Limits)
	if err != nil {
		return *new(R), err
	}
	if err := validateReplies(ctx, []Reply{reply}, s.config.Limits.Reply, false); err != nil {
		return *new(R), err
	}
	return c.decoder.decode(ctx, reply)
}
func (c Command[R]) Run(ctx context.Context, s *Store) (R, error) {
	var result R
	err := s.execute(ctx, func(ctx context.Context) error { var err error; result, err = c.run(ctx, s); return err })
	if err != nil {
		return *new(R), err
	}
	return result, nil
}

func (s *Store) Namespace() keyspace.Namespace {
	if s == nil {
		return keyspace.Namespace{}
	}
	return s.config.Namespace
}
