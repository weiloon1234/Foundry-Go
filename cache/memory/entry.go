package memory

import (
	"container/list"
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var _ cache.EntryBackend = (*Backend)(nil)
var _ cache.TaggedEntryBackend = (*Backend)(nil)
var _ cache.BatchBackend = (*Backend)(nil)
var _ cache.TaggedBatchBackend = (*Backend)(nil)

// inspectStored does not mutate storage or materialize payloads. A returned element
// with found=false is selected expired/stale storage that a successful operation
// may reclaim. Validation failure leaves live entries intact.
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
		if item.kind == taggedEntry {
			return nil, false, fault.New(fault.Invalid, "plain cache address contains a tagged entry")
		}
		return element, true, nil
	}
	if item.kind != taggedEntry {
		return nil, false, fault.New(fault.Invalid, "tagged cache entry is corrupt")
	}
	return element, item.fingerprint == tags.key.Fingerprint(), nil
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
	if ttl != nil {
		if err := ttl.Validate(); err != nil {
			return false, err
		}
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
	if ttl != nil {
		item := element.Value.(*entry)
		item.persistent = ttl.IsForever()
		item.expires = time.Time{}
		if !ttl.IsForever() {
			item.expires = now.Add(ttl.Duration())
		}
		b.trackExpiry(item)
	}
	b.order.MoveToFront(element)
	return true, nil
}
