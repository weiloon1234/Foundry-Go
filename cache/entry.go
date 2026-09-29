package cache

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Exists checks whether a live entry belongs to the current namespace/tag snapshot.
// It does not decode or copy the value; it is not validation of the application DTO.
// A later Get can miss after expiry/invalidation or fail its own codec/size checks.
// Like Get, a tagged existence check that races an invalidation re-resolves its
// snapshot (bounded) and otherwise reports false; it never returns Conflict.
func (c Cache[K, V]) Exists(ctx context.Context, key K) (bool, error) {
	started := c.startedAt()
	var found bool
	err := c.operation(ctx, func(ctx context.Context) error {
		if _, ok := c.store.backend.(EntryBackend); !ok {
			return fault.New(fault.Invalid, "cache backend does not support entry inspection")
		}
		if _, scoped := c.store.backend.(TaggedBackend); scoped {
			if _, ok := c.store.backend.(TaggedEntryBackend); !ok {
				return fault.New(fault.Invalid, "cache backend does not support tagged entry inspection")
			}
		}
		address, err := c.address(key)
		if err != nil {
			return err
		}
		memo, err := c.memoView(ctx)
		if err != nil {
			return err
		}
		if entry, ok := memo.load(address); ok {
			found = entry.found
			return nil
		}
		observed, err := c.observe(ctx, address, false)
		found = observed.found
		if err == nil && !found {
			memo.save(address, nil, false)
		}
		return err
	})
	if err != nil {
		c.report(ctx, Event{Operation: OperationExists}, started, err)
		return false, err
	}
	c.report(ctx, readEvent(OperationExists, found), started, nil)
	return found, nil
}

// Expire updates an existing entry's expiry without changing its value. Forever
// removes expiry. A present entry returns true even if the TTL did not change;
// absent/invalidated entries return false. Invalid TTLs fail before I/O. This is
// atomic with snapshot validation, not with a subsequent Get or application write.
func (c Cache[K, V]) Expire(ctx context.Context, key K, ttl TTL) (bool, error) {
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	started := c.startedAt()
	var changed bool
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {
		backend, ok := access.backend.(EntryBackend)
		if !ok {
			return fault.New(fault.Invalid, "cache backend does not support expiry changes")
		}
		var err error
		changed, err = backend.Expire(ctx, access.key, ttl)
		return err
	})
	c.report(ctx, Event{Operation: OperationExpire}, started, err)
	if err != nil {
		return false, err
	}
	return changed, nil
}

// ForgetMany atomically removes distinct live entries from this typed view. The
// input count is bounded before key encoding/deduplication. All keys are encoded
// before metadata I/O, and one snapshot protects the entire batch. No payload
// codec runs. An empty batch validates handle/context but performs no backend I/O.
// A failed remote acknowledgement returns zero and may hide an applied batch.
// Like Forget, this does not prevent a concurrent Remember from later repopulating.
func (c Cache[K, V]) ForgetMany(ctx context.Context, keys ...K) (uint64, error) {
	started := c.startedAt()
	var count uint64
	err := c.operation(ctx, func(ctx context.Context) error {
		if len(keys) > c.store.config.MaxBatchEntries {
			return fault.New(fault.Invalid, "cache batch exceeds configured key limit")
		}
		if len(keys) == 0 {
			return nil
		}
		tagged := false
		if _, ok := c.store.backend.(TaggedBackend); ok {
			tagged = true
		}
		if tagged {
			if _, ok := c.store.backend.(TaggedBatchBackend); !ok {
				return fault.New(fault.Invalid, "cache backend does not support tagged batch removal")
			}
		} else if _, ok := c.store.backend.(BatchBackend); !ok {
			return fault.New(fault.Invalid, "cache backend does not support batch removal")
		}
		bases := make([]EntryKey, 0, len(keys))
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}
			address, err := c.address(key)
			if err != nil {
				return err
			}
			bases = append(bases, address)
		}
		slices.SortFunc(bases, func(a, b EntryKey) int { return strings.Compare(a.String(), b.String()) })
		bases = slices.Compact(bases)
		if err := ctx.Err(); err != nil {
			return err
		}
		defer forgetMemo(ctx, c.store, bases...)
		snapshot, err := c.snapshot(ctx)
		if err != nil {
			return err
		}
		if snapshot.tagged == nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			count, err = c.store.backend.(BatchBackend).ForgetMany(ctx, bases)
		} else {
			batch := make([]TaggedKey, len(bases))
			for i, base := range bases {
				if err := ctx.Err(); err != nil {
					return err
				}
				batch[i], err = NewTaggedKey(base, snapshot.stamps)
				if err != nil {
					return err
				}
			}
			slices.SortFunc(batch, func(a, b TaggedKey) int { return strings.Compare(a.DataKey().String(), b.DataKey().String()) })
			if err := ctx.Err(); err != nil {
				return err
			}
			count, err = c.store.backend.(TaggedBatchBackend).ForgetManyTagged(ctx, batch)
		}
		if err == nil && count > uint64(len(bases)) {
			return fault.New(fault.Invalid, "cache backend returned an invalid batch count")
		}
		return err
	})
	c.report(ctx, Event{Operation: OperationForgetMany}, started, err)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (b taggedAccess) Exists(ctx context.Context, _ EntryKey) (bool, error) {
	backend, ok := b.backend.(TaggedEntryBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "cache backend does not support tagged entry inspection")
	}
	return backend.ExistsTagged(ctx, b.key)
}
func (b taggedAccess) Expire(ctx context.Context, _ EntryKey, ttl TTL) (bool, error) {
	backend, ok := b.backend.(TaggedEntryBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "cache backend does not support tagged expiry changes")
	}
	return backend.ExpireTagged(ctx, b.key, ttl)
}

// Exists retains the declared counter key type without decoding its value.
func (c Counter[K]) Exists(ctx context.Context, key K) (bool, error) { return c.cache.Exists(ctx, key) }

// Expire changes the counter's expiry, preserving the existing exact integer.
func (c Counter[K]) Expire(ctx context.Context, key K, ttl TTL) (bool, error) {
	return c.cache.Expire(ctx, key, ttl)
}

// ForgetMany atomically removes a bounded batch of distinct counters in this view.
func (c Counter[K]) ForgetMany(ctx context.Context, keys ...K) (uint64, error) {
	return c.cache.ForgetMany(ctx, keys...)
}
