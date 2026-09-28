package data

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type handle[K any] struct {
	store      *Store
	definition *definition[K]
}

func (h handle[K]) Name() Name {
	if h.definition == nil {
		return ""
	}
	return h.definition.name
}
func (h handle[K]) Version() Version {
	if h.definition == nil {
		return 0
	}
	return h.definition.version
}
func (h handle[K]) address(ctx context.Context, key K) (Key, error) {
	text, err := h.definition.keys.Encode(key)
	if err != nil {
		return Key{}, err
	}
	if err := ctx.Err(); err != nil {
		return Key{}, err
	}
	if len(text) > h.store.config.MaxKeyBytes {
		return Key{}, fault.New(fault.Invalid, "Redis data key exceeds its bound")
	}
	return NewKey(h.store.config.Namespace, h.Name(), h.Version(), h.definition.kind, text)
}
func (h handle[K]) execute(ctx context.Context, key K, fn func(context.Context, Key, Limits) error) error {
	if err := h.definition.validate(); err != nil {
		return err
	}
	return h.store.execute(ctx, func(ctx context.Context) error {
		address, err := h.address(ctx, key)
		if err != nil {
			return err
		}
		return fn(ctx, address, h.store.config.Limits)
	})
}

// Exists checks the key's declared Redis type, without decoding payloads.
func (h handle[K]) Exists(ctx context.Context, key K) (bool, error) {
	var result bool
	err := h.execute(ctx, key, func(ctx context.Context, k Key, _ Limits) error {
		var err error
		result, err = h.store.backend.DataExists(ctx, k)
		return err
	})
	if err != nil {
		return false, err
	}
	return result, nil
}

// Expire changes expiry without replacing data. New hash/set writes are persistent
// and later writes retain TTL. Setting expiry is a separate explicit operation;
// a write followed by Expire does not claim atomic initialization with expiry.
func (h handle[K]) Expire(ctx context.Context, key K, ttl cache.TTL) (bool, error) {
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	var result bool
	err := h.execute(ctx, key, func(ctx context.Context, k Key, _ Limits) error {
		var err error
		result, err = h.store.backend.DataExpire(ctx, k, ttl)
		return err
	})
	if err != nil {
		return false, err
	}
	return result, nil
}
func (h handle[K]) Count(ctx context.Context, key K) (uint64, error) {
	var count uint64
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		var err error
		count, err = h.store.backend.DataCount(ctx, k, l)
		if err != nil {
			return err
		}
		if count > uint64(l.Entries) {
			return fault.New(fault.Internal, "invalid Redis data cardinality")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func (h handle[K]) Delete(ctx context.Context, key K) (bool, error) {
	n, err := h.DeleteMany(ctx, key)
	return n == 1, err
}

// DeleteMany validates all encoded keys before one atomic batch. The input bound
// applies before deduplication; returned count excludes duplicates and missing keys.
func (h handle[K]) DeleteMany(ctx context.Context, keys ...K) (uint64, error) {
	if err := h.definition.validate(); err != nil {
		return 0, err
	}
	var count uint64
	err := h.store.execute(ctx, func(ctx context.Context) error {
		if len(keys) > h.store.config.MaxBatchKeys {
			return fault.New(fault.Invalid, "Redis data batch exceeds its bound")
		}
		if len(keys) == 0 {
			return nil
		}
		addresses := make([]Key, len(keys))
		for i, key := range keys {
			var err error
			addresses[i], err = h.address(ctx, key)
			if err != nil {
				return err
			}
		}
		slices.SortFunc(addresses, func(a, b Key) int { return strings.Compare(a.String(), b.String()) })
		addresses = slices.Compact(addresses)
		var err error
		count, err = h.store.backend.DataDeleteMany(ctx, addresses)
		if err != nil {
			return err
		}
		if count > uint64(len(addresses)) {
			return fault.New(fault.Internal, "invalid Redis data deletion count")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// AdapterKey explicitly exports this declaration's resolved address for advanced
// Redis adapters. It preserves key typing and callback ownership; ordinary callers
// should prefer the typed Hash/Set operations.
func (h handle[K]) AdapterKey(ctx context.Context, input K) (Key, error) {
	var key Key
	err := h.execute(ctx, input, func(_ context.Context, resolved Key, _ Limits) error { key = resolved; return nil })
	if err != nil {
		return Key{}, err
	}
	return key, nil
}
