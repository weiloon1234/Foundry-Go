package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Invalidate logically invalidates every typed cache entry in this namespace,
// including tagged views and counters across Store instances. It requires a
// TaggedBackend. Native memory/Redis stores apply namespace protection automatically.
//
// Rotation is atomic. A stale operation cannot publish or delete a newer value;
// overlapping operations may return fault.Conflict. An in-flight Remember loader
// retains ownership until it exits, then fails publication if its snapshot changed.
// New calls use independent fill identities. No loader or mutation is retried.
//
// This is not physical erasure: stale payloads expire or are reclaimed on access,
// replacement or adapter eviction, using stable addresses even for Forever data.
// Tag metadata, leases, rate limits, pub/sub and other namespaces are not cleared.
// Direct adapter writes bypass this typed-store contract. A remote error may hide
// an applied rotation; it does not prove that invalidation was rolled back.
func (s *Store) Invalidate(ctx context.Context) error {
	return s.invalidate(ctx, func() ([]EntryKey, error) {
		key, err := NewNamespaceTagKey(s.config.Namespace)
		return []EntryKey{key}, err
	})
}

func (s *Store) invalidate(ctx context.Context, resolve func() ([]EntryKey, error)) error {
	backend, err := tagCapability(s)
	if err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "cache invalidation requires a context")
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
		return fault.Wrap(fault.Internal, "cache invalidation failed", err)
	}
	return operation.Err()
}
