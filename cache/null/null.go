// Package null supplies a cache backend that retains nothing, for disabling a
// cache store without changing its typed declarations. Every read misses and
// every write succeeds without storing, so Remember always runs its loader.
package null

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Backend implements every typed cache capability without storage, so stores
// requiring tags, counters, entries or batches accept it. Inputs are validated
// like any adapter's. Add reports true (the entry was absent) and Increment
// returns delta (a counter starting at zero), although nothing is retained;
// neither can serve as a lock or an accumulator. Tag resolution returns fresh
// versions each call. Construction performs no I/O and there is nothing to close.
type Backend struct{}

var (
	_ cache.Backend                = Backend{}
	_ cache.CounterBackend         = Backend{}
	_ cache.EntryBackend           = Backend{}
	_ cache.BatchBackend           = Backend{}
	_ cache.BatchReadBackend       = Backend{}
	_ cache.TaggedBackend          = Backend{}
	_ cache.TaggedCounterBackend   = Backend{}
	_ cache.TaggedEntryBackend     = Backend{}
	_ cache.TaggedBatchBackend     = Backend{}
	_ cache.TaggedBatchReadBackend = Backend{}
)

// New returns a stateless backend that retains no entries.
func New() Backend { return Backend{} }

// FoundryAdapter marks the backend as framework-owned adapter I/O.
func (Backend) FoundryAdapter(frameworkadapter.Seal) {}

func operation(ctx context.Context, key cache.EntryKey) error {
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "null cache operation requires a context")
	}
	if err := key.Validate(); err != nil {
		return err
	}
	return ctx.Err()
}
func tagged(ctx context.Context, key cache.TaggedKey) error {
	if err := key.Validate(); err != nil {
		return err
	}
	return operation(ctx, key.DataKey())
}
func write(ctx context.Context, key cache.EntryKey, ttl cache.TTL) error {
	if err := ttl.Validate(); err != nil {
		return err
	}
	return operation(ctx, key)
}

// Get validates the operation and always reports a miss.
func (Backend) Get(ctx context.Context, key cache.EntryKey) ([]byte, bool, error) {
	return nil, false, operation(ctx, key)
}

// Put validates the operation and TTL without retaining data.
func (Backend) Put(ctx context.Context, key cache.EntryKey, _ []byte, ttl cache.TTL) error {
	return write(ctx, key, ttl)
}

// Add validates the operation and TTL and reports success without retaining data.
func (Backend) Add(ctx context.Context, key cache.EntryKey, _ []byte, ttl cache.TTL) (bool, error) {
	if err := write(ctx, key, ttl); err != nil {
		return false, err
	}
	return true, nil
}

// Forget validates the operation and reports that no entry was removed.
func (Backend) Forget(ctx context.Context, key cache.EntryKey) (bool, error) {
	return false, operation(ctx, key)
}

// Increment validates the operation and TTL and returns delta without retaining a counter.
func (Backend) Increment(ctx context.Context, key cache.EntryKey, delta int64, ttl cache.TTL) (int64, error) {
	if err := write(ctx, key, ttl); err != nil {
		return 0, err
	}
	return cacheint.Add(0, delta)
}

// Exists validates the operation and reports false.
func (Backend) Exists(ctx context.Context, key cache.EntryKey) (bool, error) {
	return false, operation(ctx, key)
}

// Expire validates the operation and TTL and reports false.
func (Backend) Expire(ctx context.Context, key cache.EntryKey, ttl cache.TTL) (bool, error) {
	return false, write(ctx, key, ttl)
}

// ForgetMany validates the batch and context and reports zero removals.
func (Backend) ForgetMany(ctx context.Context, keys []cache.EntryKey) (uint64, error) {
	if err := cache.ValidateBatchKeys(keys); err != nil {
		return 0, err
	}
	return 0, live(ctx)
}

// GetMany validates the batch and context and returns a miss for every input.
func (Backend) GetMany(ctx context.Context, keys []cache.EntryKey) ([]cache.BatchValue, error) {
	if err := cache.ValidateBatchKeys(keys); err != nil {
		return nil, err
	}
	if err := live(ctx); err != nil {
		return nil, err
	}
	return make([]cache.BatchValue, len(keys)), nil
}

// ResolveTags returns fresh versions: nothing tagged is ever stored.
func (Backend) ResolveTags(ctx context.Context, keys []cache.EntryKey) ([]cache.TagVersion, error) {
	if err := cache.ValidateTagKeys(keys); err != nil {
		return nil, err
	}
	if err := live(ctx); err != nil {
		return nil, err
	}
	versions := make([]cache.TagVersion, len(keys))
	for i := range versions {
		version, err := cache.NewTagVersion()
		if err != nil {
			return nil, err
		}
		versions[i] = version
	}
	return versions, nil
}

// InvalidateTags validates the tag keys and context without retaining metadata.
func (Backend) InvalidateTags(ctx context.Context, keys []cache.EntryKey) error {
	if err := cache.ValidateTagKeys(keys); err != nil {
		return err
	}
	return live(ctx)
}

// GetTagged validates the tagged operation and reports a miss.
func (Backend) GetTagged(ctx context.Context, key cache.TaggedKey) ([]byte, bool, error) {
	return nil, false, tagged(ctx, key)
}

// PutTagged validates the tagged operation and TTL without retaining data.
func (Backend) PutTagged(ctx context.Context, key cache.TaggedKey, _ []byte, ttl cache.TTL) error {
	if err := ttl.Validate(); err != nil {
		return err
	}
	return tagged(ctx, key)
}

// AddTagged validates the tagged operation and TTL and reports success without retaining data.
func (b Backend) AddTagged(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL) (bool, error) {
	if err := b.PutTagged(ctx, key, data, ttl); err != nil {
		return false, err
	}
	return true, nil
}

// ForgetTagged validates the tagged operation and reports false.
func (Backend) ForgetTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	return false, tagged(ctx, key)
}

// IncrementTagged validates the tagged operation and TTL and returns delta without retaining a counter.
func (Backend) IncrementTagged(ctx context.Context, key cache.TaggedKey, delta int64, ttl cache.TTL) (int64, error) {
	if err := ttl.Validate(); err != nil {
		return 0, err
	}
	if err := tagged(ctx, key); err != nil {
		return 0, err
	}
	return cacheint.Add(0, delta)
}

// ExistsTagged validates the tagged operation and reports false.
func (Backend) ExistsTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	return false, tagged(ctx, key)
}

// ExpireTagged validates the tagged operation and TTL and reports false.
func (Backend) ExpireTagged(ctx context.Context, key cache.TaggedKey, ttl cache.TTL) (bool, error) {
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	return false, tagged(ctx, key)
}

// ForgetManyTagged validates the tagged batch and context and reports zero removals.
func (Backend) ForgetManyTagged(ctx context.Context, keys []cache.TaggedKey) (uint64, error) {
	if err := cache.ValidateTaggedBatch(keys); err != nil {
		return 0, err
	}
	return 0, live(ctx)
}

// GetManyTagged validates the tagged batch and context and returns a miss for every input.
func (Backend) GetManyTagged(ctx context.Context, keys []cache.TaggedKey) ([]cache.BatchValue, error) {
	if err := cache.ValidateTaggedBatch(keys); err != nil {
		return nil, err
	}
	if err := live(ctx); err != nil {
		return nil, err
	}
	return make([]cache.BatchValue, len(keys)), nil
}

func live(ctx context.Context) error {
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "null cache operation requires a context")
	}
	return ctx.Err()
}
