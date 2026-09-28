package memory

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
)

var _ cache.CounterBackend = (*Backend)(nil)

// Increment changes the same entry used by the other cache operations, under the
// same mutex. Existing expiry is preserved and failures leave its data unchanged.
func (b *Backend) Increment(ctx context.Context, key cache.EntryKey, delta int64, initialTTL cache.TTL) (int64, error) {
	if err := b.validate(ctx, key); err != nil {
		return 0, err
	}
	if err := initialTTL.Validate(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return 0, err
	}
	now := b.clock.Now()
	return b.incrementEntry(ctx, key, delta, initialTTL, now, nil)
}
func (b *Backend) incrementEntry(ctx context.Context, key cache.EntryKey, delta int64, initialTTL cache.TTL, now time.Time, tags *tagState) (int64, error) {
	previous := b.lookup(key, now)
	var value int64
	if previous != nil && tags != nil {
		item := previous.Value.(*entry)
		if item.kind != taggedEntry {
			return 0, fault.New(fault.Invalid, "tagged counter is corrupt")
		}
		if item.fingerprint != tags.key.Fingerprint() {
			previous = nil
		}
	}
	if previous != nil {
		var err error
		value, err = cacheint.Decode(previous.Value.(*entry).data)
		if err != nil {
			return 0, err
		}
	}
	value, err := cacheint.Add(value, delta)
	if err != nil {
		return 0, err
	}
	data := cacheint.Encode(value)
	cost, err := b.entryCost(key, len(data))
	if tags != nil {
		cost, err = b.taggedCost(key, len(data))
	}
	if err != nil {
		return 0, err
	}
	if tags != nil {
		if err := b.tagBudget(tags, cost); err != nil {
			return 0, err
		}
	}
	item := makeEntry(key, data, cost, initialTTL, now)
	if tags != nil {
		item.kind = taggedEntry
		item.fingerprint = tags.key.Fingerprint()
	}
	if previous != nil {
		stored := previous.Value.(*entry)
		item.expires, item.persistent = stored.expires, stored.persistent
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if tags != nil {
		b.protectTags(tags)
	}
	b.retain(item, now)
	return value, nil
}
