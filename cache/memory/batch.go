package memory

import (
	"container/list"
	"context"

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
