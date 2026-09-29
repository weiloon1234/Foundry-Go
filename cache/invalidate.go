package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/faultwrap"
)

// Invalidate logically invalidates every typed cache entry in this namespace,
// including tagged views and counters across Store instances. TaggedBackend
// stores (memory, Redis) rotate a reserved namespace generation; FlushBackend
// stores (file, PostgreSQL) physically remove the namespace's entries instead.
// Basic adapters without either capability return fault.Invalid.
//
// Rotation is atomic. A stale operation cannot publish or delete a newer value;
// overlapping operations may return fault.Conflict. An in-flight Remember loader
// retains ownership until it exits, then fails publication if its snapshot changed
// (its callers still receive the loaded value; the failure is reported).
// New calls use independent fill identities. No loader or mutation is retried.
// A physical flush is not a fence: a fill that started earlier may publish after it.
//
// This is not physical erasure for tagged stores: stale payloads expire or are
// reclaimed on access, replacement or adapter eviction, using stable addresses
// even for Forever data. Tag metadata, leases, rate limits, pub/sub and other
// namespaces are not cleared. Direct adapter writes bypass this typed-store
// contract. A remote error may hide an applied rotation; it does not prove that
// invalidation was rolled back.
func (s *Store) Invalidate(ctx context.Context) error {
	started := s.started()
	if ctx != nil {
		defer forgetMemo(ctx, s)
	}
	var err error
	if flusher, ok := s.flusher(); ok {
		err = s.flush(ctx, flusher)
	} else {
		err = s.invalidate(ctx, func() ([]EntryKey, error) {
			return []EntryKey{s.namespaceTag}, nil
		})
	}
	s.report(ctx, Event{Operation: OperationInvalidate}, started, err)
	return err
}

// flusher returns the FlushBackend of an adapter without tag metadata.
func (s *Store) flusher() (FlushBackend, bool) {
	if s == nil || s.backend == nil {
		return nil, false
	}
	if _, tagged := s.backend.(TaggedBackend); tagged {
		return nil, false
	}
	flusher, ok := s.backend.(FlushBackend)
	return flusher, ok
}

func (s *Store) flush(ctx context.Context, backend FlushBackend) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "cache invalidation requires a context")
	}
	if err := s.active(); err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if err := operation.Err(); err != nil {
		return err
	}
	err := callback.Isolated("cache namespace flush", func() error {
		_, err := backend.FlushNamespace(operation, s.config.Namespace)
		return err
	})
	if err != nil {
		return faultwrap.Wrap("cache invalidation failed", err)
	}
	return operation.Err()
}

func (s *Store) invalidate(ctx context.Context, resolve func() ([]EntryKey, error)) error {
	backend, err := tagCapability(s)
	if err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "cache invalidation requires a context")
	}
	if err := s.active(); err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if err := operation.Err(); err != nil {
		return err
	}
	err = callback.Isolated("cache invalidation", func() error {
		keys, err := resolve()
		if err != nil {
			return err
		}
		if err := operation.Err(); err != nil {
			return err
		}
		return backend.InvalidateTags(operation, keys)
	})
	if err != nil {
		return faultwrap.Wrap("cache invalidation failed", err)
	}
	return operation.Err()
}
