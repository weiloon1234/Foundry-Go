package memory

import (
	"container/list"
	"context"
	"crypto/sha256"
	"slices"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (b *Backend) taggedCost(key cache.EntryKey, size int) (int, error) {
	cost, err := b.entryCost(key, size)
	if err != nil {
		return 0, err
	}
	if b.config.MaxBytes < sha256.Size || cost > b.config.MaxBytes-sha256.Size {
		return 0, fault.New(fault.Invalid, "tagged entry exceeds memory byte budget")
	}
	return cost + sha256.Size, nil
}
func (b *Backend) GetTagged(ctx context.Context, key cache.TaggedKey) ([]byte, bool, error) {
	if err := b.validateTagged(ctx, key); err != nil {
		return nil, false, err
	}
	b.mu.RLock()
	if err := b.active(ctx); err != nil {
		b.mu.RUnlock()
		return nil, false, err
	}
	now := b.clock.Now()
	state, err := b.checkTags(key, now)
	if err != nil {
		b.mu.RUnlock()
		return nil, false, err
	}
	b.referenceTags(state)
	element, found, err := b.inspectStored(key.DataKey(), state, now)
	if err != nil || !found {
		b.mu.RUnlock()
		if element != nil {
			// Expired, obsolete or unusable storage is a miss that is reclaimed.
			b.reclaim([]cache.EntryKey{key.DataKey()}, []*list.Element{element}, true)
		}
		return nil, false, err
	}
	touch(element)
	data := element.Value.(*entry).data
	b.mu.RUnlock()
	// Retained data is immutable, so the owned copy is made outside the lock.
	return slices.Clone(data), true, nil
}
func (b *Backend) PutTagged(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL) error {
	_, err := b.writeTagged(ctx, key, data, ttl, false)
	return err
}
func (b *Backend) AddTagged(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL) (bool, error) {
	return b.writeTagged(ctx, key, data, ttl, true)
}
func (b *Backend) writeTagged(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL, onlyAbsent bool) (bool, error) {
	if err := b.validateTagged(ctx, key); err != nil {
		return false, err
	}
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	cost, err := b.taggedCost(key.DataKey(), len(data))
	if err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return false, err
	}
	now := b.clock.Now()
	state, err := b.checkTags(key, now)
	if err != nil {
		return false, err
	}
	if err := b.tagBudget(state, cost); err != nil {
		return false, err
	}
	// An entry of another kind at this address is unusable and is replaced.
	if previous := b.lookup(key.DataKey(), now); previous != nil {
		item := previous.Value.(*entry)
		if onlyAbsent && item.kind == taggedEntry && item.fingerprint == key.Fingerprint() {
			return false, nil
		}
	}
	item := makeEntry(key.DataKey(), data, cost, ttl, now)
	item.kind = taggedEntry
	item.fingerprint = key.Fingerprint()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.protectTags(state)
	b.retain(item, now)
	return true, nil
}
func (b *Backend) ForgetTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	if err := b.validateTagged(ctx, key); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return false, err
	}
	now := b.clock.Now()
	state, err := b.checkTags(key, now)
	if err != nil {
		return false, err
	}
	b.protectTags(state)
	element := b.lookup(key.DataKey(), now)
	if element == nil {
		return false, nil
	}
	item := element.Value.(*entry)
	found := item.kind == taggedEntry && item.fingerprint == key.Fingerprint()
	b.remove(element)
	return found, nil
}
func (b *Backend) IncrementTagged(ctx context.Context, key cache.TaggedKey, delta int64, ttl cache.TTL) (int64, error) {
	if err := b.validateTagged(ctx, key); err != nil {
		return 0, err
	}
	if err := ttl.Validate(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return 0, err
	}
	now := b.clock.Now()
	state, err := b.checkTags(key, now)
	if err != nil {
		return 0, err
	}
	return b.incrementEntry(ctx, key.DataKey(), delta, ttl, now, state)
}
