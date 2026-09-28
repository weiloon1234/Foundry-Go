package cache

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
)

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
func resolveTagKeys(tags []Tag) ([]EntryKey, error) {
	keys := make([]EntryKey, 0, len(tags))
	for _, tag := range tags {
		key, err := tag.address()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b EntryKey) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(keys), nil
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
	return s.invalidate(ctx, func() ([]EntryKey, error) { return resolveTagKeys(tags) })
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

func (c Cache[K, V]) snapshot(ctx context.Context) (accessSnapshot, error) {
	if _, ok := c.store.backend.(TaggedBackend); !ok && len(c.tags) == 0 {
		return accessSnapshot{backend: c.store.backend}, nil
	}
	backend, err := tagCapability(c.store)
	if err != nil {
		return accessSnapshot{}, err
	}
	keys, err := resolveTagKeys(c.tags)
	if err != nil {
		return accessSnapshot{}, err
	}
	scope, err := NewNamespaceTagKey(c.store.config.Namespace)
	if err != nil {
		return accessSnapshot{}, err
	}
	keys = append(keys, scope)
	slices.SortFunc(keys, func(a, b EntryKey) int { return strings.Compare(a.String(), b.String()) })
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
	return entryAccess{key: tagged.DataKey(), fillKey: tagged.FillKey(), backend: taggedAccess{backend: s.tagged, key: tagged}}, nil
}
func (c Cache[K, V]) access(ctx context.Context, base EntryKey) (entryAccess, error) {
	snapshot, err := c.snapshot(ctx)
	if err != nil {
		return entryAccess{}, err
	}
	return snapshot.bind(base)
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
