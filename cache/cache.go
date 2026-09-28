package cache

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Cache accepts only its declared key and value types. Values are serialized
// snapshots; reading a hit does not return a reference into backend storage.
// The zero value rejects operations. Use Declaration.Bind to construct a handle.
type Cache[K, V any] struct {
	store      *Store
	definition *definition[K, V]
	tags       []Tag
}

func (c Cache[K, V]) operation(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil || c.store == nil || c.definition == nil {
		return fault.New(fault.Invalid, "cache operation needs an initialized handle and context")
	}
	operation, cancel := context.WithTimeout(ctx, c.store.config.Timeout)
	defer cancel()
	if err := operation.Err(); err != nil {
		return err
	}
	err := callback.Isolated("cache operation", func() error { return fn(operation) })
	if err != nil {
		return fault.Wrap(fault.Internal, "cache operation failed", err)
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
		access, err := c.access(ctx, address)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
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
func (c Cache[K, V]) Get(ctx context.Context, key K) (V, bool, error) {
	var value V
	var found bool
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		data, present, err := c.read(ctx, access)
		if err != nil || !present {
			return err
		}
		value, err = c.decode(ctx, data)
		found = err == nil
		return err
	})
	if err != nil {
		return *new(V), false, err
	}
	return value, found, nil
}

// read and decode are shared by Get and Remember; their caller owns isolation.
func (c Cache[K, V]) read(ctx context.Context, access entryAccess) ([]byte, bool, error) {
	data, present, err := access.backend.Get(ctx, access.key)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if !present {
		return nil, false, nil
	}
	if len(data) > c.store.config.MaxValueBytes {
		return nil, false, fault.New(fault.Invalid, "cached value exceeds its decode limit")
	}
	return slices.Clone(data), true, nil
}
func (c Cache[K, V]) decode(ctx context.Context, data []byte) (V, error) {
	if err := ctx.Err(); err != nil {
		return *new(V), err
	}
	return c.definition.values.decode(slices.Clone(data))
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
	return c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		data, err := c.encode(ctx, value, ttl)
		if err != nil {
			return err
		}
		return access.backend.Put(ctx, access.key, data, ttl)
	})
}

// Add atomically inserts only when no unexpired entry exists. False means an
// existing value was retained, including its original TTL. Encoding precedes Add.
func (c Cache[K, V]) Add(ctx context.Context, key K, value V, ttl TTL) (bool, error) {
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	var added bool
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		data, err := c.encode(ctx, value, ttl)
		if err != nil {
			return err
		}
		added, err = access.backend.Add(ctx, access.key, data, ttl)
		return err
	})
	if err != nil {
		return false, err
	}
	return added, nil
}
func (c Cache[K, V]) Forget(ctx context.Context, key K) (bool, error) {
	var removed bool
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		var err error
		removed, err = access.backend.Forget(ctx, access.key)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
