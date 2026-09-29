package memory

import (
	"container/list"
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/cache"
)

func (b *Backend) ForgetMany(ctx context.Context, keys []cache.EntryKey) (uint64, error) {
	if err := cache.ValidateBatchKeys(keys); err != nil {
		return 0, err
	}
	return b.removeBatch(ctx, keys, nil)
}
func (b *Backend) ForgetManyTagged(ctx context.Context, keys []cache.TaggedKey) (uint64, error) {
	if err := cache.ValidateTaggedBatch(keys); err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return b.removeBatch(ctx, nil, nil)
	}
	bases := make([]cache.EntryKey, len(keys))
	for i, key := range keys {
		bases[i] = key.DataKey()
	}
	return b.removeBatch(ctx, bases, &keys[0])
}
func (b *Backend) removeBatch(ctx context.Context, keys []cache.EntryKey, snapshot *cache.TaggedKey) (uint64, error) {
	if err := b.validateContext(ctx); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return 0, err
	}
	now := b.clock.Now()
	var tags *tagState
	if snapshot != nil {
		var err error
		tags, err = b.checkTags(*snapshot, now)
		if err != nil {
			return 0, err
		}
	}
	planned := make([]*list.Element, 0, len(keys))
	var count uint64
	for _, key := range keys {
		element, found, err := b.inspectStored(key, tags, now)
		if err != nil {
			return 0, err
		}
		if element != nil {
			planned = append(planned, element)
		}
		if found {
			count++
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if tags != nil {
		b.protectTags(tags)
	}
	for _, element := range planned {
		b.remove(element)
	}
	return count, nil
}

var _ cache.BatchReadBackend = (*Backend)(nil)
var _ cache.TaggedBatchReadBackend = (*Backend)(nil)

// GetMany reads a canonical batch under one shared lock; results follow input order.
func (b *Backend) GetMany(ctx context.Context, keys []cache.EntryKey) ([]cache.BatchValue, error) {
	if err := cache.ValidateBatchKeys(keys); err != nil {
		return nil, err
	}
	return b.readBatch(ctx, keys, nil)
}

// GetManyTagged validates one shared snapshot and reads the batch atomically.
func (b *Backend) GetManyTagged(ctx context.Context, keys []cache.TaggedKey) ([]cache.BatchValue, error) {
	if err := cache.ValidateTaggedBatch(keys); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return b.readBatch(ctx, nil, nil)
	}
	bases := make([]cache.EntryKey, len(keys))
	for i, key := range keys {
		bases[i] = key.DataKey()
	}
	return b.readBatch(ctx, bases, &keys[0])
}
func (b *Backend) readBatch(ctx context.Context, keys []cache.EntryKey, snapshot *cache.TaggedKey) ([]cache.BatchValue, error) {
	if err := b.validateContext(ctx); err != nil {
		return nil, err
	}
	values := make([]cache.BatchValue, len(keys))
	b.mu.RLock()
	if err := b.active(ctx); err != nil {
		b.mu.RUnlock()
		return nil, err
	}
	now := b.clock.Now()
	var tags *tagState
	if snapshot != nil {
		var err error
		if tags, err = b.checkTags(*snapshot, now); err != nil {
			b.mu.RUnlock()
			return nil, err
		}
		b.referenceTags(tags)
	}
	var staleKeys []cache.EntryKey
	var stale []*list.Element
	for i, key := range keys {
		element, found, err := b.inspectStored(key, tags, now)
		if err != nil {
			b.mu.RUnlock()
			return nil, err
		}
		if !found {
			if element != nil {
				staleKeys, stale = append(staleKeys, key), append(stale, element)
			}
			continue
		}
		touch(element)
		values[i] = cache.BatchValue{Data: element.Value.(*entry).data, Found: true}
	}
	b.mu.RUnlock()
	// Expired, obsolete or unusable storage is a miss that is reclaimed.
	if len(stale) != 0 {
		b.reclaim(staleKeys, stale, true)
	}
	// Retained data is immutable; owned copies are made outside the lock.
	for i := range values {
		if values[i].Found {
			values[i].Data = slices.Clone(values[i].Data)
		}
	}
	return values, nil
}
