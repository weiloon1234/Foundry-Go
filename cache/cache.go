package cache

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/faultwrap"
)

// Cache accepts only its declared key and value types. Values are serialized
// snapshots; reading a hit does not return a reference into backend storage.
// The zero value rejects operations. Use Declaration.Bind to construct a handle.
type Cache[K, V any] struct {
	store      *Store
	definition *definition[K, V]
	tags       []Tag
}

func (c Cache[K, V]) valid(ctx context.Context) error {
	if ctx == nil || c.store == nil || c.definition == nil {
		return fault.New(fault.Invalid, "cache operation needs an initialized handle and context")
	}
	return c.store.active()
}

// operation bounds one store operation by Config.Timeout. Failures keep the
// framework classification of their cause (for example Overloaded, Invalid,
// Timeout or Closed) behind a safe cache message.
func (c Cache[K, V]) operation(ctx context.Context, fn func(context.Context) error) error {
	if err := c.valid(ctx); err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, c.store.config.Timeout)
	defer cancel()
	if err := operation.Err(); err != nil {
		return err
	}
	// Key codecs, value codecs and adapters can all be application code.
	err := callback.Isolated("cache operation", func() error { return fn(operation) })
	if err != nil {
		return faultwrap.Wrap("cache operation failed", err)
	}
	return operation.Err()
}
func (c Cache[K, V]) execute(ctx context.Context, key K, fn func(context.Context, entryAccess) error) error {
	return c.operation(ctx, func(ctx context.Context) error {
		address, err := c.address(key)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		access, err := c.writeAccess(ctx, address)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// A failed remote write may still have applied: always forget the key.
		defer forgetMemo(ctx, c.store, address)
		return fn(ctx, access)
	})
}

func (c Cache[K, V]) address(key K) (EntryKey, error) {
	if c.store == nil || c.definition == nil {
		return EntryKey{}, fault.New(fault.Invalid, "cache address is not initialized")
	}
	logical, err := c.definition.keys.Encode(key)
	if err != nil {
		return EntryKey{}, err
	}
	if len(logical) > c.store.config.MaxKeyBytes {
		return EntryKey{}, fault.New(fault.Invalid, "encoded cache key exceeds its limit")
	}
	return NewEntryKey(c.store.config.Namespace, c.definition.name, logical)
}

// Get returns found=true for every stored value, including nil or a zero V.
// Backend/decoder failure returns zero V, false and an error; it is never a miss.
// A tagged read that races an invalidation re-resolves its snapshot (bounded)
// and otherwise reports a miss; a pure read never returns a snapshot conflict.
// A stored value larger than the current MaxValueBytes (written under an
// earlier, larger bound) is a miss that a later write replaces.
func (c Cache[K, V]) Get(ctx context.Context, key K) (V, bool, error) {
	started := c.startedAt()
	var value V
	var found bool
	err := c.operation(ctx, func(ctx context.Context) error {
		address, err := c.address(key)
		if err != nil {
			return err
		}
		memo, err := c.memoView(ctx)
		if err != nil {
			return err
		}
		if entry, ok := memo.load(address); ok {
			if !entry.found {
				return nil
			}
			value, err = c.decode(ctx, slices.Clone(entry.data))
			found = err == nil
			return err
		}
		observed, err := c.observe(ctx, address, true)
		if err != nil {
			return err
		}
		memo.save(address, observed.data, observed.found)
		if !observed.found {
			return nil
		}
		value, err = c.decode(ctx, observed.data)
		found = err == nil
		return err
	})
	if err != nil {
		c.report(ctx, Event{Operation: OperationGet}, started, err)
		return *new(V), false, err
	}
	c.store.countRead(found)
	c.report(ctx, readEvent(OperationGet, found), started, nil)
	return value, found, nil
}

// read is shared by Remember fills; its caller owns isolation. Returned bytes
// are owned by the caller. An over-bound value is a miss (see Get).
func (c Cache[K, V]) read(ctx context.Context, access entryAccess) ([]byte, bool, error) {
	data, present, err := access.backend.Get(ctx, access.key)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if !present || len(data) > c.store.config.MaxValueBytes {
		return nil, false, nil
	}
	return data, true, nil
}

// decode receives bytes owned by this call; codecs must not modify them.
func (c Cache[K, V]) decode(ctx context.Context, data []byte) (V, error) {
	if err := ctx.Err(); err != nil {
		return *new(V), err
	}
	return c.definition.values.decode(data)
}

func (c Cache[K, V]) encode(ctx context.Context, value V, ttl TTL) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ttl.Validate(); err != nil {
		return nil, err
	}
	data, err := c.definition.values.encode(value)
	if err != nil {
		return nil, err
	}
	if len(data) > c.store.config.MaxValueBytes {
		return nil, fault.New(fault.Invalid, "encoded cache value exceeds its limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Clone(data), nil
}

// Put replaces one entry with the supplied TTL. No implicit command retry occurs.
func (c Cache[K, V]) Put(ctx context.Context, key K, value V, ttl TTL) error {
	if err := ttl.Validate(); err != nil {
		return err
	}
	started := c.startedAt()
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		data, err := c.encode(ctx, value, ttl)
		if err != nil {
			return err
		}
		return access.backend.Put(ctx, access.key, data, ttl)
	})
	if err == nil {
		c.store.counters.writes.Add(1)
	}
	c.report(ctx, Event{Operation: OperationPut}, started, err)
	return err
}

// Add atomically inserts only when no unexpired entry exists. False means an
// existing value was retained, including its original TTL. Encoding precedes Add.
func (c Cache[K, V]) Add(ctx context.Context, key K, value V, ttl TTL) (bool, error) {
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	started := c.startedAt()
	var added bool
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		data, err := c.encode(ctx, value, ttl)
		if err != nil {
			return err
		}
		added, err = access.backend.Add(ctx, access.key, data, ttl)
		return err
	})
	c.report(ctx, Event{Operation: OperationAdd}, started, err)
	if err != nil {
		return false, err
	}
	if added {
		c.store.counters.writes.Add(1)
	}
	return added, nil
}
func (c Cache[K, V]) Forget(ctx context.Context, key K) (bool, error) {
	started := c.startedAt()
	var removed bool
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		var err error
		removed, err = access.backend.Forget(ctx, access.key)
		return err
	})
	c.report(ctx, Event{Operation: OperationForget}, started, err)
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (s *Store) countRead(hit bool) {
	if hit {
		s.counters.hits.Add(1)
	} else {
		s.counters.misses.Add(1)
	}
}
