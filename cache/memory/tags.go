package memory

import (
	"container/list"
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var _ cache.TaggedBackend = (*Backend)(nil)
var _ cache.TaggedCounterBackend = (*Backend)(nil)

func (b *Backend) validateTagKeys(ctx context.Context, keys []cache.EntryKey) error {
	if err := cache.ValidateTagKeys(keys); err != nil {
		return err
	}
	return b.validate(ctx, keys[0])
}

// ResolveTags reads existing metadata under the shared lock and takes the
// exclusive lock only when a version must be created.
func (b *Backend) ResolveTags(ctx context.Context, keys []cache.EntryKey) ([]cache.TagVersion, error) {
	if err := b.validateTagKeys(ctx, keys); err != nil {
		return nil, err
	}
	versions, complete, err := b.existingVersions(ctx, keys)
	if err != nil || complete {
		return versions, err
	}
	return b.versions(ctx, keys, false)
}

// existingVersions returns complete=true when every key has live metadata. It
// never mutates storage; referenced metadata gets a second chance at eviction.
func (b *Backend) existingVersions(ctx context.Context, keys []cache.EntryKey) ([]cache.TagVersion, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.active(ctx); err != nil {
		return nil, false, err
	}
	now := b.clock.Now()
	elements := make([]*list.Element, len(keys))
	versions := make([]cache.TagVersion, len(keys))
	for i, key := range keys {
		element := b.live(key, now)
		if element == nil {
			return nil, false, nil
		}
		item := element.Value.(*entry)
		if item.kind != tagMetadata {
			return nil, false, fault.New(fault.Invalid, "cache tag metadata is corrupt")
		}
		version, err := cache.ParseTagVersion(item.data)
		if err != nil {
			return nil, false, err
		}
		elements[i], versions[i] = element, version
	}
	for _, element := range elements {
		touch(element)
	}
	return versions, true, nil
}
func (b *Backend) InvalidateTags(ctx context.Context, keys []cache.EntryKey) error {
	_, err := b.versions(ctx, keys, true)
	return err
}

// Plan every metadata replacement before applying any live mutation. Existing
// selected entries are moved ahead of eviction candidates before replacements.
func (b *Backend) versions(ctx context.Context, keys []cache.EntryKey, replace bool) ([]cache.TagVersion, error) {
	if err := b.validateTagKeys(ctx, keys); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return nil, err
	}
	if len(keys) > b.config.MaxEntries {
		return nil, fault.New(fault.Invalid, "tag set exceeds memory entry budget")
	}
	now := b.clock.Now()
	items := make([]*entry, len(keys))
	versions := make([]cache.TagVersion, len(keys))
	created := make([]bool, len(keys))
	total := 0
	for i, key := range keys {
		var item *entry
		if old := b.lookup(key, now); old != nil {
			item = old.Value.(*entry)
			if item.kind != tagMetadata {
				return nil, fault.New(fault.Invalid, "cache tag metadata is corrupt")
			}
			version, err := cache.ParseTagVersion(item.data)
			if err != nil {
				return nil, err
			}
			versions[i] = version
		}
		if item == nil || replace {
			version, err := cache.NewTagVersion()
			if err != nil {
				return nil, err
			}
			cost, err := b.entryCost(key, cache.TagVersionBytes)
			if err != nil {
				return nil, err
			}
			item = makeEntry(key, version.Bytes(), cost, cache.Forever(), now)
			item.kind = tagMetadata
			versions[i] = version
			created[i] = true
		}
		if item.cost > b.config.MaxBytes-total {
			return nil, fault.New(fault.Invalid, "tag set exceeds memory byte budget")
		}
		total += item.cost
		items[i] = item
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if element := b.entries[key]; element != nil {
			b.order.MoveToFront(element)
		}
	}
	// Unchanged metadata was only moved to the front above; retain (and charge)
	// just the entries that are new or replaced.
	for i, item := range items {
		if created[i] {
			b.retain(item, now)
		}
	}
	return versions, nil
}

type tagState struct {
	key      cache.TaggedKey
	elements []*list.Element
	bytes    int
}

func (b *Backend) checkTags(key cache.TaggedKey, now time.Time) (*tagState, error) {
	state := &tagState{key: key}
	for _, stamp := range key.Stamps() {
		// Non-mutating so read paths can check under the shared lock.
		element := b.live(stamp.Key, now)
		if element == nil {
			return nil, fault.New(fault.Conflict, "cache tag snapshot expired")
		}
		item := element.Value.(*entry)
		if item.kind != tagMetadata {
			return nil, fault.New(fault.Invalid, "cache tag metadata is corrupt")
		}
		version, err := cache.ParseTagVersion(item.data)
		if err != nil {
			return nil, err
		}
		if version != stamp.Version {
			return nil, fault.New(fault.Conflict, "cache tag snapshot changed")
		}
		state.bytes += item.cost
		state.elements = append(state.elements, element)
	}
	return state, nil
}
func (b *Backend) protectTags(state *tagState) {
	for _, element := range state.elements {
		b.order.MoveToFront(element)
	}
}

// referenceTags is protectTags for read paths under the shared lock.
func (b *Backend) referenceTags(state *tagState) {
	for _, element := range state.elements {
		touch(element)
	}
}
func (b *Backend) tagBudget(state *tagState, cost int) error {
	if len(state.elements) >= b.config.MaxEntries || cost > b.config.MaxBytes-state.bytes {
		return fault.New(fault.Invalid, "tagged entry and metadata exceed memory budget")
	}
	return nil
}
func (b *Backend) validateTagged(ctx context.Context, key cache.TaggedKey) error {
	if err := key.Validate(); err != nil {
		return err
	}
	return b.validate(ctx, key.DataKey())
}
