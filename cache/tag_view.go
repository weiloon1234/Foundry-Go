package cache

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// snapshotAttempts bounds how often a pure read re-resolves a tag snapshot that
// a concurrent invalidation replaced before it reports a miss.
const snapshotAttempts = 3

func tagCapability(store *Store) (TaggedBackend, error) {
	if store == nil || store.backend == nil {
		return nil, fault.New(fault.Invalid, "cache tags require an initialized store")
	}
	backend, ok := store.backend.(TaggedBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "cache backend does not support atomic tags")
	}
	if store.coordination != nil {
		if _, ok := backend.(CoordinatedTaggedBackend); !ok {
			return nil, fault.New(fault.Invalid, "cache backend does not support coordinated tags")
		}
	}
	return backend, nil
}
func (s *Store) validateTags(tags []Tag) error {
	if s == nil || len(tags) == 0 || len(tags) > s.config.MaxTags {
		return fault.New(fault.Invalid, "invalid cache tag count")
	}
	for _, tag := range tags {
		if tag.store != s || tag.address == nil {
			return fault.New(fault.Invalid, "cache tag belongs to another store or is uninitialized")
		}
	}
	return nil
}

// canonicalKeys sorts and deduplicates by physical address, rendering each
// address once instead of once per comparison.
func canonicalKeys(keys []EntryKey) []EntryKey {
	type rendered struct {
		key  EntryKey
		text string
	}
	items := make([]rendered, len(keys))
	for i, key := range keys {
		items[i] = rendered{key, key.String()}
	}
	slices.SortFunc(items, func(a, b rendered) int { return strings.Compare(a.text, b.text) })
	result := keys[:0]
	for i, item := range items {
		if i > 0 && item.text == items[i-1].text {
			continue
		}
		result = append(result, item.key)
	}
	return result
}
func resolveTagKeys(tags []Tag) ([]EntryKey, error) {
	keys := make([]EntryKey, 0, len(tags)+1)
	for _, tag := range tags {
		key, err := tag.address()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return canonicalKeys(keys), nil
}

// WithTags returns a view with the supplied tag set; it replaces any earlier set.
// At least one tag is required. Construction validates ownership/capability without
// encoding keys or performing I/O. Operations share the same typed API and Store.
func (c Cache[K, V]) WithTags(first Tag, rest ...Tag) (Cache[K, V], error) {
	if c.store == nil || c.definition == nil {
		return Cache[K, V]{}, fault.New(fault.Invalid, "cache view is not initialized")
	}
	if len(rest) >= c.store.config.MaxTags {
		return Cache[K, V]{}, fault.New(fault.Invalid, "cache tag count exceeds its limit")
	}
	tags := append([]Tag{first}, rest...)
	if err := c.store.validateTags(tags); err != nil {
		return Cache[K, V]{}, err
	}
	if _, err := tagCapability(c.store); err != nil {
		return Cache[K, V]{}, err
	}
	c.tags = tags
	return c, nil
}

// InvalidateTags atomically invalidates entries containing any selected reference.
// A remote error may hide an applied invalidation; no implicit retry occurs.
func (s *Store) InvalidateTags(ctx context.Context, first Tag, rest ...Tag) error {
	if s == nil || len(rest) >= s.config.MaxTags {
		return fault.New(fault.Invalid, "invalid cache store or tag count")
	}
	tags := append([]Tag{first}, rest...)
	if err := s.validateTags(tags); err != nil {
		return err
	}
	started := s.started()
	if ctx != nil {
		defer forgetMemo(ctx, s)
	}
	err := s.invalidate(ctx, func() ([]EntryKey, error) { return resolveTagKeys(tags) })
	s.report(ctx, Event{Operation: OperationInvalidate}, started, err)
	return err
}

type entryAccess struct {
	key     EntryKey
	fillKey EntryKey
	backend Backend
}

type accessSnapshot struct {
	backend Backend
	tagged  TaggedBackend
	stamps  []TagStamp
}

// scoped reports whether operations carry a tag snapshot (explicit tags or the
// automatic namespace stamp of a TaggedBackend).
func (c Cache[K, V]) scoped() bool {
	_, ok := c.store.backend.(TaggedBackend)
	return ok || len(c.tags) > 0
}

// snapshotKeys returns the canonical metadata addresses for this view: its
// application tags plus the store's reserved namespace stamp.
func (c Cache[K, V]) snapshotKeys() ([]EntryKey, error) {
	keys, err := resolveTagKeys(c.tags)
	if err != nil {
		return nil, err
	}
	return canonicalKeys(append(keys, c.store.namespaceTag)), nil
}

func (c Cache[K, V]) snapshot(ctx context.Context) (accessSnapshot, error) {
	if !c.scoped() {
		return accessSnapshot{backend: c.store.backend}, nil
	}
	backend, err := tagCapability(c.store)
	if err != nil {
		return accessSnapshot{}, err
	}
	keys, err := c.snapshotKeys()
	if err != nil {
		return accessSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return accessSnapshot{}, err
	}
	versions, err := backend.ResolveTags(ctx, keys)
	if err != nil {
		return accessSnapshot{}, err
	}
	if len(versions) != len(keys) {
		return accessSnapshot{}, fault.New(fault.Invalid, "cache backend returned an invalid tag snapshot")
	}
	stamps := make([]TagStamp, len(keys))
	for i, key := range keys {
		stamps[i] = TagStamp{Key: key, Version: versions[i]}
	}

	return accessSnapshot{backend: c.store.backend, tagged: backend, stamps: stamps}, nil
}
func (s accessSnapshot) bind(base EntryKey) (entryAccess, error) {
	if s.tagged == nil {
		return entryAccess{key: base, fillKey: base, backend: s.backend}, nil
	}
	tagged, err := NewTaggedKey(base, s.stamps)
	if err != nil {
		return entryAccess{}, err
	}
	return taggedEntryAccess(s.tagged, tagged), nil
}
func taggedEntryAccess(backend TaggedBackend, key TaggedKey) entryAccess {
	return entryAccess{key: key.DataKey(), fillKey: key.FillKey(), backend: taggedAccess{backend: backend, key: key}}
}
func (c Cache[K, V]) access(ctx context.Context, base EntryKey) (entryAccess, error) {
	snapshot, err := c.snapshot(ctx)
	if err != nil {
		return entryAccess{}, err
	}
	return snapshot.bind(base)
}

// writeAccess binds a direct mutation (Put, Add, Forget, Increment, Expire).
// A SnapshotWriteBackend resolves the snapshot inside the mutation itself, so
// the write costs one round trip and cannot cross an invalidation.
func (c Cache[K, V]) writeAccess(ctx context.Context, base EntryKey) (entryAccess, error) {
	writer, ok := c.store.backend.(SnapshotWriteBackend)
	if !ok || !c.scoped() {
		return c.access(ctx, base)
	}
	if _, err := tagCapability(c.store); err != nil {
		return entryAccess{}, err
	}
	keys, err := c.snapshotKeys()
	if err != nil {
		return entryAccess{}, err
	}
	return entryAccess{key: base, fillKey: base, backend: snapshotAccess{writer: writer, reader: c.store.backend, base: base, tags: keys}}, nil
}

// snapshotAccess adapts SnapshotWriteBackend to the per-entry capabilities used
// by direct mutations. Reads go through observe; Get/Exists here only delegate
// to an optional SnapshotReadBackend.
type snapshotAccess struct {
	writer SnapshotWriteBackend
	reader Backend
	base   EntryKey
	tags   []EntryKey
}

func (a snapshotAccess) Get(ctx context.Context, _ EntryKey) ([]byte, bool, error) {
	reader, ok := a.reader.(SnapshotReadBackend)
	if !ok {
		return nil, false, fault.New(fault.Invalid, "cache backend does not support snapshot reads")
	}
	_, data, found, err := reader.ReadSnapshot(ctx, a.base, a.tags, true)
	return data, found, err
}
func (a snapshotAccess) Exists(ctx context.Context, _ EntryKey) (bool, error) {
	reader, ok := a.reader.(SnapshotReadBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "cache backend does not support snapshot reads")
	}
	_, _, found, err := reader.ReadSnapshot(ctx, a.base, a.tags, false)
	return found, err
}
func (a snapshotAccess) Put(ctx context.Context, _ EntryKey, data []byte, ttl TTL) error {
	return a.writer.PutSnapshot(ctx, a.base, a.tags, data, ttl)
}
func (a snapshotAccess) Add(ctx context.Context, _ EntryKey, data []byte, ttl TTL) (bool, error) {
	return a.writer.AddSnapshot(ctx, a.base, a.tags, data, ttl)
}
func (a snapshotAccess) Forget(ctx context.Context, _ EntryKey) (bool, error) {
	return a.writer.ForgetSnapshot(ctx, a.base, a.tags)
}
func (a snapshotAccess) Increment(ctx context.Context, _ EntryKey, delta int64, ttl TTL) (int64, error) {
	return a.writer.IncrementSnapshot(ctx, a.base, a.tags, delta, ttl)
}
func (a snapshotAccess) Expire(ctx context.Context, _ EntryKey, ttl TTL) (bool, error) {
	return a.writer.ExpireSnapshot(ctx, a.base, a.tags, ttl)
}

// observation is one pure read: the snapshot it used and, when found, owned bytes.
type observation struct {
	access entryAccess
	data   []byte
	found  bool
}

// observe performs a pure read (Get, Exists or Remember's lookup). Adapters with
// SnapshotReadBackend resolve metadata and read in one round trip. Otherwise a
// snapshot that a concurrent invalidation replaced is re-resolved a bounded
// number of times and then reported as a miss: reads never fail with Conflict.
func (c Cache[K, V]) observe(ctx context.Context, base EntryKey, payload bool) (observation, error) {
	if reader, ok := c.store.backend.(SnapshotReadBackend); ok && c.scoped() {
		backend, err := tagCapability(c.store)
		if err != nil {
			return observation{}, err
		}
		keys, err := c.snapshotKeys()
		if err != nil {
			return observation{}, err
		}
		if err := ctx.Err(); err != nil {
			return observation{}, err
		}
		key, data, found, err := reader.ReadSnapshot(ctx, base, keys, payload)
		if err != nil {
			return observation{}, err
		}
		if !key.resolves(base, keys) {
			return observation{}, fault.New(fault.Invalid, "cache backend returned an invalid tag snapshot")
		}
		result := observation{access: taggedEntryAccess(backend, key), found: found}
		if found && payload {
			if len(data) > c.store.config.MaxValueBytes {
				result.found = false
			} else {
				result.data = data
			}
		}
		return result, ctx.Err()
	}
	for attempt := 1; ; attempt++ {
		access, err := c.access(ctx, base)
		if err != nil {
			return observation{}, err
		}
		if err := ctx.Err(); err != nil {
			return observation{}, err
		}
		result := observation{access: access}
		if payload {
			result.data, result.found, err = c.read(ctx, access)
		} else {
			result.found, err = c.exists(ctx, access)
		}
		if err == nil || !c.scoped() || !errorgraph.Is(err, fault.Conflict) {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return observation{}, err
		}
		if attempt == snapshotAttempts {
			return observation{access: access}, nil
		}
		c.store.counters.snapshotRetries.Add(1)
	}
}

func (c Cache[K, V]) exists(ctx context.Context, access entryAccess) (bool, error) {
	backend, ok := access.backend.(EntryBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "cache backend does not support entry inspection")
	}
	return backend.Exists(ctx, access.key)
}

type taggedAccess struct {
	backend TaggedBackend
	key     TaggedKey
}

func (b taggedAccess) Get(ctx context.Context, _ EntryKey) ([]byte, bool, error) {
	return b.backend.GetTagged(ctx, b.key)
}
func (b taggedAccess) Put(ctx context.Context, _ EntryKey, data []byte, ttl TTL) error {
	return b.backend.PutTagged(ctx, b.key, data, ttl)
}
func (b taggedAccess) Add(ctx context.Context, _ EntryKey, data []byte, ttl TTL) (bool, error) {
	return b.backend.AddTagged(ctx, b.key, data, ttl)
}
func (b taggedAccess) Forget(ctx context.Context, _ EntryKey) (bool, error) {
	return b.backend.ForgetTagged(ctx, b.key)
}
func (b taggedAccess) Increment(ctx context.Context, _ EntryKey, delta int64, ttl TTL) (int64, error) {
	backend, ok := b.backend.(TaggedCounterBackend)
	if !ok {
		return 0, fault.New(fault.Invalid, "cache backend does not support tagged counters")
	}
	return backend.IncrementTagged(ctx, b.key, delta, ttl)
}

func (b taggedAccess) PutLeased(ctx context.Context, _ EntryKey, data []byte, ttl TTL, proof lease.Proof) error {
	backend, ok := b.backend.(CoordinatedTaggedBackend)
	if !ok {
		return fault.New(fault.Invalid, "cache backend does not support coordinated tags")
	}
	return backend.PutTaggedLeased(ctx, b.key, data, ttl, proof)
}
