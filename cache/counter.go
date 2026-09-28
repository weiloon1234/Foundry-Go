package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Counter retains its declared key type through exact int64 reads and mutations.
// It is an evictable cache counter, not durable accounting or a rate limiter.
// Operations use the Store's namespace, key codec, timeout and error handling.
// Construct it with CounterDeclaration.Bind; its zero value rejects operations.
type Counter[K any] struct {
	cache   Cache[K, int64]
	backend CounterBackend
}

// Get separates a missing entry from a stored zero. Corrupt or noncanonical data
// returns an error, never a miss or a rounded integer.
func (c Counter[K]) Get(ctx context.Context, key K) (int64, bool, error) {
	return c.cache.Get(ctx, key)
}

// Put replaces the counter and its TTL atomically.
func (c Counter[K]) Put(ctx context.Context, key K, value int64, ttl TTL) error {
	return c.cache.Put(ctx, key, value, ttl)
}

// Add initializes only an absent or expired counter. Existing value/TTL remain.
func (c Counter[K]) Add(ctx context.Context, key K, value int64, ttl TTL) (bool, error) {
	return c.cache.Add(ctx, key, value, ttl)
}

func (c Counter[K]) Forget(ctx context.Context, key K) (bool, error) { return c.cache.Forget(ctx, key) }

// Increment atomically adds a signed delta and returns the resulting value. A
// negative delta subtracts. Missing/expired values start at zero with initialTTL;
// existing entries retain their original expiry, even for delta=0. The initialTTL
// must always be valid. Overflow/corruption fails without modifying the entry.
func (c Counter[K]) Increment(ctx context.Context, key K, delta int64, initialTTL TTL) (int64, error) {
	if err := initialTTL.Validate(); err != nil {
		return 0, err
	}
	var value int64
	err := c.cache.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		if c.backend == nil {
			return fault.New(fault.Invalid, "cache counter is not initialized")
		}
		var err error
		backend, ok := access.backend.(CounterBackend)
		if !ok {
			return fault.New(fault.Invalid, "cache backend does not support counters")
		}
		value, err = backend.Increment(ctx, access.key, delta, initialTTL)
		return err
	})
	if err != nil {
		return 0, err
	}
	return value, nil
}

// Decrement subtracts a nonnegative amount. Use Increment for a signed delta.
func (c Counter[K]) Decrement(ctx context.Context, key K, amount int64, initialTTL TTL) (int64, error) {
	if amount < 0 {
		return 0, fault.New(fault.Invalid, "cache counter decrement requires a nonnegative amount")
	}
	return c.Increment(ctx, key, -amount, initialTTL)
}

// WithTags retains atomic counter operations while applying the selected tags.
func (c Counter[K]) WithTags(first Tag, rest ...Tag) (Counter[K], error) {
	view, err := c.cache.WithTags(first, rest...)
	if err != nil {
		return Counter[K]{}, err
	}
	if _, ok := view.store.backend.(TaggedCounterBackend); !ok {
		return Counter[K]{}, fault.New(fault.Invalid, "cache backend does not support tagged counters")
	}
	c.cache = view
	return c, nil
}
