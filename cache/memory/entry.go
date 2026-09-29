package memory

import (
	"container/list"
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
)

var _ cache.EntryBackend = (*Backend)(nil)
var _ cache.TaggedEntryBackend = (*Backend)(nil)
var _ cache.BatchBackend = (*Backend)(nil)
var _ cache.TaggedBatchBackend = (*Backend)(nil)

// inspectStored does not mutate storage or materialize payloads. A returned element
// with found=false is expired, stale or unusable storage (an entry of the other
// kind at this address) that a successful operation may reclaim: an unusable
// entry is a miss, never a failure, and later writes replace it.
func (b *Backend) inspectStored(key cache.EntryKey, tags *tagState, now time.Time) (*list.Element, bool, error) {
	element := b.entries[key]
	if element == nil {
		return nil, false, nil
	}
	item := element.Value.(*entry)
	if expired(item, now) {
		return element, false, nil
	}
	if tags == nil {
		return element, item.kind != taggedEntry, nil
	}
	return element, item.kind == taggedEntry && item.fingerprint == tags.key.Fingerprint(), nil
}

func (b *Backend) Exists(ctx context.Context, key cache.EntryKey) (bool, error) {
	return b.entryOperation(ctx, key, nil, nil)
}
func (b *Backend) Expire(ctx context.Context, key cache.EntryKey, ttl cache.TTL) (bool, error) {
	return b.entryOperation(ctx, key, nil, &ttl)
}
func (b *Backend) ExistsTagged(ctx context.Context, key cache.TaggedKey) (bool, error) {
	if err := key.Validate(); err != nil {
		return false, err
	}
	return b.entryOperation(ctx, key.DataKey(), &key, nil)
}
func (b *Backend) ExpireTagged(ctx context.Context, key cache.TaggedKey, ttl cache.TTL) (bool, error) {
	if err := key.Validate(); err != nil {
		return false, err
	}
	return b.entryOperation(ctx, key.DataKey(), &key, &ttl)
}
func (b *Backend) entryOperation(ctx context.Context, key cache.EntryKey, tagged *cache.TaggedKey, ttl *cache.TTL) (bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return false, err
	}
	if ttl == nil {
		return b.exists(ctx, key, tagged)
	}
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return false, err
	}
	now := b.clock.Now()
	var tags *tagState
	if tagged != nil {
		var err error
		tags, err = b.checkTags(*tagged, now)
		if err != nil {
			return false, err
		}
	}
	element, found, err := b.inspectStored(key, tags, now)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tags != nil {
		b.protectTags(tags)
	}
	if !found {
		if element != nil {
			b.remove(element)
		}
		return false, nil
	}
	item := element.Value.(*entry)
	item.persistent = ttl.IsForever()
	item.expires = time.Time{}
	if !ttl.IsForever() {
		item.expires = now.Add(ttl.Duration())
	}
	b.trackExpiry(item)
	b.order.MoveToFront(element)
	return true, nil
}

// exists inspects one entry under the shared lock without reordering storage.
// Expired, obsolete or unusable storage is a miss that is then reclaimed.
func (b *Backend) exists(ctx context.Context, key cache.EntryKey, tagged *cache.TaggedKey) (bool, error) {
	element, found, err := b.inspectShared(ctx, key, tagged)
	if element != nil && !found && err == nil {
		b.reclaim([]cache.EntryKey{key}, []*list.Element{element}, true)
	}
	return found, err
}
func (b *Backend) inspectShared(ctx context.Context, key cache.EntryKey, tagged *cache.TaggedKey) (*list.Element, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.active(ctx); err != nil {
		return nil, false, err
	}
	now := b.clock.Now()
	var tags *tagState
	if tagged != nil {
		var err error
		if tags, err = b.checkTags(*tagged, now); err != nil {
			return nil, false, err
		}
		b.referenceTags(tags)
	}
	element, found, err := b.inspectStored(key, tags, now)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if found {
		touch(element)
	}
	return element, found, nil
}
