package memory_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func tagKey(t *testing.T, name, logical string) cache.EntryKey {
	t.Helper()
	key, err := cache.NewEntryKey(cache.Namespace{Application: "tags", Environment: "test"}, cache.Name(name), logical)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func tagBackend(t *testing.T, config memory.Config) *memory.Backend {
	t.Helper()
	b, err := memory.New(config, testkit.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return b
}
func snapshot(t *testing.T, b *memory.Backend, base cache.EntryKey, keys ...cache.EntryKey) cache.TaggedKey {
	t.Helper()
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	versions, err := b.ResolveTags(t.Context(), keys)
	if err != nil {
		t.Fatal(err)
	}
	stamps := make([]cache.TagStamp, len(keys))
	for i, key := range keys {
		stamps[i] = cache.TagStamp{Key: key, Version: versions[i]}
	}
	result, err := cache.NewTaggedKey(base, stamps)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestMissingMetadataCannotResurrectOldDataOrDeleteNewData(t *testing.T) {
	b := tagBackend(t, memory.DefaultConfig())
	base := tagKey(t, "profiles", "key")
	tag := tagKey(t, "teams", "a")
	old := snapshot(t, b, base, tag)
	if err := b.PutTagged(t.Context(), old, []byte("old"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if removed, err := b.Forget(t.Context(), tag); err != nil || !removed {
		t.Fatal(removed, err)
	}
	fresh := snapshot(t, b, base, tag)
	if old.DataKey() != fresh.DataKey() || old.FillKey() == fresh.FillKey() {
		t.Fatal("data/fill identity mismatch")
	}
	if data, found, err := b.GetTagged(t.Context(), fresh); err != nil || found {
		t.Fatal("metadata recreation exposed old data", string(data), found, err)
	}
	if err := b.PutTagged(t.Context(), fresh, []byte("new"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.GetTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
		t.Fatal("stale read accepted", err)
	}
	if _, err := b.ForgetTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
		t.Fatal("stale cleanup accepted", err)
	}
	if err := b.PutTagged(t.Context(), old, []byte("late"), cache.Forever()); !errors.Is(err, fault.Conflict) {
		t.Fatal("stale write accepted", err)
	}
	if data, found, err := b.GetTagged(t.Context(), fresh); err != nil || !found || string(data) != "new" {
		t.Fatal("new replacement lost", string(data), found, err)
	}
}
func TestActualMetadataEvictionProducesFreshIdentity(t *testing.T) {
	config := memory.DefaultConfig()
	config.MaxEntries = 2
	b := tagBackend(t, config)
	base := tagKey(t, "profiles", "key")
	tag := tagKey(t, "teams", "a")
	old := snapshot(t, b, base, tag)
	if err := b.PutTagged(t.Context(), old, []byte("old"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	// Payload is newer than metadata, so ordinary pressure evicts metadata first.
	if err := b.Put(t.Context(), tagKey(t, "unrelated", "pressure"), []byte("x"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	fresh := snapshot(t, b, base, tag)
	if fresh.FillKey() == old.FillKey() {
		t.Fatal("evicted metadata reused a generation")
	}
	if _, found, err := b.GetTagged(t.Context(), fresh); err != nil || found {
		t.Fatal("eviction resurrected stale data", found, err)
	}
}
func TestTagCapacityRejectionPreservesLiveData(t *testing.T) {
	base := tagKey(t, "profiles", "key")
	tag := tagKey(t, "teams", "a")
	config := memory.DefaultConfig()
	config.MaxEntries = 2
	config.MaxBytes = len(tag.String()) + cache.TagVersionBytes + len(base.String()) + 32 + 3
	b := tagBackend(t, config)
	key := snapshot(t, b, base, tag)
	if err := b.PutTagged(t.Context(), key, []byte("old"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := b.PutTagged(t.Context(), key, []byte("longer"), cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if data, found, err := b.GetTagged(t.Context(), key); err != nil || !found || string(data) != "old" {
		t.Fatal(string(data), found, err)
	}
	if stats := b.Stats(); stats.Entries != 2 || stats.Bytes != config.MaxBytes {
		t.Fatal("metadata/fingerprint budget not charged", stats)
	}
	tooSmall := memory.DefaultConfig()
	tooSmall.MaxEntries = 1
	small := tagBackend(t, tooSmall)
	k := snapshot(t, small, base, tag)
	if err := small.PutTagged(t.Context(), k, []byte("x"), cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal("write evicted its own tag", err)
	}
	before := small.Stats()
	keys := []cache.EntryKey{tag, tagKey(t, "teams", "b")}
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	if err := small.InvalidateTags(t.Context(), keys); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if after := small.Stats(); after != before {
		t.Fatal("failed invalidation changed live entries", before, after)
	}
}
func TestTagMetadataCorruptionAndInvalidSnapshotsFail(t *testing.T) {
	b := tagBackend(t, memory.DefaultConfig())
	base := tagKey(t, "profiles", "key")
	tag := tagKey(t, "teams", "a")
	old := snapshot(t, b, base, tag)
	if err := b.Put(t.Context(), tag, []byte("not tag metadata"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ResolveTags(t.Context(), []cache.EntryKey{tag}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := b.InvalidateTags(t.Context(), []cache.EntryKey{tag}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, _, err := b.GetTagged(t.Context(), old); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := b.ResolveTags(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := b.ResolveTags(t.Context(), []cache.EntryKey{tag, tag}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := b.PutTagged(t.Context(), cache.TaggedKey{}, nil, cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestFailedBatchInvalidationDoesNotPartiallyRotateMetadata(t *testing.T) {
	b := tagBackend(t, memory.DefaultConfig())
	base := tagKey(t, "profiles", "key")
	keys := []cache.EntryKey{tagKey(t, "teams", "a"), tagKey(t, "teams", "b")}
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	old := snapshot(t, b, base, keys...)
	before := old.Stamps()[0].Version
	if err := b.Put(t.Context(), keys[1], []byte("corrupt"), cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if err := b.InvalidateTags(t.Context(), keys); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	versions, err := b.ResolveTags(t.Context(), keys[:1])
	if err != nil || versions[0] != before {
		t.Fatal("failed batch rotated its first tag", versions, err)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.InvalidateTags(t.Context(), keys[:1]); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	if _, _, err := b.GetTagged(t.Context(), old); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}
