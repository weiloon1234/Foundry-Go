package cachetest

import (
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type TaggedBackend interface {
	Backend
	cache.TaggedBackend
	cache.TaggedCounterBackend
}
type TaggedFixture struct {
	Backend TaggedBackend
	Key     func(string) cache.EntryKey
	// Track registers exact derived payload addresses for adapter-owned cleanup.
	Track func(cache.EntryKey)
}

func (f TaggedFixture) Snapshot(t *testing.T, base cache.EntryKey, keys ...cache.EntryKey) cache.TaggedKey {
	t.Helper()
	keys = slices.Clone(keys)
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	keys = slices.Compact(keys)
	versions, err := f.Backend.ResolveTags(t.Context(), keys)
	if err != nil {
		t.Fatal(err)
	}
	stamps := make([]cache.TagStamp, len(keys))
	for i, key := range keys {
		stamps[i] = cache.TagStamp{Key: key, Version: versions[i]}
	}
	snapshot, err := cache.NewTaggedKey(base, stamps)
	if err != nil {
		t.Fatal(err)
	}
	f.Track(snapshot.DataKey())
	return snapshot
}
func RunTagged(t *testing.T, setup func(*testing.T) TaggedFixture) {
	t.Helper()
	t.Run("invalid-batches-and-cancellation", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		keys := []cache.EntryKey{f.Key("a"), f.Key("b")}
		slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
		foreign, err := cache.NewEntryKey(cache.Namespace{Application: "other", Environment: "test"}, "tags", "foreign")
		if err != nil {
			t.Fatal(err)
		}
		for _, invalid := range [][]cache.EntryKey{nil, {{}}, {keys[0], keys[0]}, {keys[1], keys[0]}, {keys[0], foreign}, make([]cache.EntryKey, cache.MaxTags+1)} {
			if _, err := b.ResolveTags(t.Context(), invalid); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
			if err := b.InvalidateTags(t.Context(), invalid); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := b.ResolveTags(ctx, keys); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := b.InvalidateTags(ctx, keys); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		for _, key := range keys {
			if _, hit, err := b.Get(t.Context(), key); err != nil || hit {
				t.Fatal("rejected batch changed metadata", hit, err)
			}
		}
	})

	t.Run("owned-tagged-values", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		k := f.Snapshot(t, f.Key("value"), f.Key("tag"))
		if _, hit, err := b.GetTagged(t.Context(), k); err != nil || hit {
			t.Fatal(hit, err)
		}
		for _, data := range [][]byte{nil, {}, []byte("owned")} {
			expected := string(data)
			if err := b.PutTagged(t.Context(), k, data, cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if len(data) > 0 {
				data[0] = 'X'
			}
			got, hit, err := b.GetTagged(t.Context(), k)
			if err != nil || !hit || string(got) != expected {
				t.Fatal(string(got), hit, err)
			}
			if len(got) > 0 {
				got[0] = 'Y'
			}
			got, _, err = b.GetTagged(t.Context(), k)
			if err != nil || string(got) != expected {
				t.Fatal("read aliases storage", err)
			}
			if added, err := b.AddTagged(t.Context(), k, []byte("replacement"), cache.Forever()); err != nil || added {
				t.Fatal(added, err)
			}
		}
		if removed, err := b.ForgetTagged(t.Context(), k); err != nil || !removed {
			t.Fatal(removed, err)
		}
		if removed, err := b.ForgetTagged(t.Context(), k); err != nil || removed {
			t.Fatal(removed, err)
		}
	})
	t.Run("invalidation-isolates-tag-sets", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		base := f.Key("base")
		a := f.Key("a")
		z := f.Key("z")
		one := f.Snapshot(t, base, a)
		two := f.Snapshot(t, base, a, z)
		other := f.Snapshot(t, base, z)
		for _, k := range []cache.TaggedKey{one, two, other} {
			if err := b.PutTagged(t.Context(), k, []byte("before"), cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		if err := b.InvalidateTags(t.Context(), []cache.EntryKey{a}); err != nil {
			t.Fatal(err)
		}
		for _, k := range []cache.TaggedKey{f.Snapshot(t, base, a), f.Snapshot(t, base, a, z)} {
			if _, hit, err := b.GetTagged(t.Context(), k); err != nil || hit {
				t.Fatal("invalidated view hit", hit, err)
			}
		}
		if got, hit, err := b.GetTagged(t.Context(), other); err != nil || !hit || string(got) != "before" {
			t.Fatal("unrelated set changed", hit, err)
		}
	})
	t.Run("stale-snapshot-cannot-overwrite-or-clean-newer-data", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		base := f.Key("base")
		tag := f.Key("tag")
		old := f.Snapshot(t, base, tag)
		if err := b.PutTagged(t.Context(), old, []byte("old"), cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := b.InvalidateTags(t.Context(), []cache.EntryKey{tag}); err != nil {
			t.Fatal(err)
		}
		fresh := f.Snapshot(t, base, tag)
		if fresh.DataKey() != old.DataKey() || fresh.FillKey() == old.FillKey() {
			t.Fatal("generation identity mismatch")
		}
		if ok, err := b.AddTagged(t.Context(), fresh, []byte("12"), cache.Forever()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if _, _, err := b.GetTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if err := b.PutTagged(t.Context(), old, []byte("late"), cache.Forever()); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.AddTagged(t.Context(), old, []byte("late"), cache.Forever()); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.ForgetTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.IncrementTagged(t.Context(), old, 1, cache.Forever()); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if got, hit, err := b.GetTagged(t.Context(), fresh); err != nil || !hit || string(got) != "12" {
			t.Fatal("new value lost", hit, err)
		}
	})
	t.Run("missing-metadata-never-resurrects", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		base := f.Key("base")
		tag := f.Key("tag")
		old := f.Snapshot(t, base, tag)
		if err := b.PutTagged(t.Context(), old, []byte("obsolete"), cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Forget(t.Context(), tag); err != nil {
			t.Fatal(err)
		}
		if _, _, err := b.GetTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		fresh := f.Snapshot(t, base, tag)
		if old.FillKey() == fresh.FillKey() {
			t.Fatal("metadata identity reused")
		}
		if _, hit, err := b.GetTagged(t.Context(), fresh); err != nil || hit {
			t.Fatal("obsolete value resurrected", hit, err)
		}
	})
	t.Run("counter-precision-and-contention", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		k := f.Snapshot(t, f.Key("counter"), f.Key("tag"))
		for _, initial := range []int64{1<<53 + 1, math.MaxInt64, math.MinInt64} {
			if err := b.PutTagged(t.Context(), k, []byte(strconv.FormatInt(initial, 10)), cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if got, err := b.IncrementTagged(t.Context(), k, 0, cache.Forever()); err != nil || got != initial {
				t.Fatal(got, initial, err)
			}
			if initial == math.MaxInt64 || initial == math.MinInt64 {
				delta := int64(1)
				if initial < 0 {
					delta = -1
				}
				if _, err := b.IncrementTagged(t.Context(), k, delta, cache.Forever()); !errors.Is(err, fault.Invalid) {
					t.Fatal(err)
				}
				if got, err := b.IncrementTagged(t.Context(), k, 0, cache.Forever()); err != nil || got != initial {
					t.Fatal("overflow changed value", got, err)
				}
			}
		}
		if _, err := b.ForgetTagged(t.Context(), k); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				if _, err := b.IncrementTagged(t.Context(), k, 1, cache.For(time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if got, err := b.IncrementTagged(t.Context(), k, 0, cache.Forever()); err != nil || got != 64 {
			t.Fatal(got, err)
		}
	})
	t.Run("current-counter-corruption-and-obsolete-payload", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		base := f.Key("counter")
		tag := f.Key("tag")
		snapshot := f.Snapshot(t, base, tag)
		for _, raw := range []string{"01", "-0", "9223372036854775808"} {
			if err := b.PutTagged(t.Context(), snapshot, []byte(raw), cache.For(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := b.IncrementTagged(t.Context(), snapshot, 1, cache.Forever()); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
			if got, hit, err := b.GetTagged(t.Context(), snapshot); err != nil || !hit || string(got) != raw {
				t.Fatal("counter corruption changed", hit, err)
			}
		}
		if err := b.PutTagged(t.Context(), snapshot, []byte(strings.Repeat("x", 100)), cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := b.InvalidateTags(t.Context(), []cache.EntryKey{tag}); err != nil {
			t.Fatal(err)
		}
		fresh := f.Snapshot(t, base, tag)
		if got, err := b.IncrementTagged(t.Context(), fresh, 2, cache.For(time.Minute)); err != nil || got != 2 {
			t.Fatal("obsolete value was decoded as a counter", got, err)
		}
	})

	t.Run("batch-corruption-never-partially-rotates", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		keys := []cache.EntryKey{f.Key("a"), f.Key("b")}
		slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
		before, err := b.ResolveTags(t.Context(), keys)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Put(t.Context(), keys[1], []byte("corrupt"), cache.For(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := b.InvalidateTags(t.Context(), keys); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		after, err := b.ResolveTags(t.Context(), keys[:1])
		if err != nil || after[0] != before[0] {
			t.Fatal("partial rotation", err)
		}
		if _, err := b.Forget(t.Context(), keys[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := b.ResolveTags(t.Context(), keys); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, hit, err := b.Get(t.Context(), keys[0]); err != nil || hit {
			t.Fatal("partial initialization", hit, err)
		}
	})
	t.Run("concurrent-resolve-publishes-one-identity", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		keys := []cache.EntryKey{f.Key("tag")}
		versions := make(chan cache.TagVersion, 64)
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				v, err := b.ResolveTags(t.Context(), keys)
				if err != nil {
					t.Error(err)
					return
				}
				versions <- v[0]
			})
		}
		wg.Wait()
		close(versions)
		var expected cache.TagVersion
		for version := range versions {
			if expected == (cache.TagVersion{}) {
				expected = version
			}
			if version != expected {
				t.Fatal("concurrent initializations diverged")
			}
		}
	})
}
