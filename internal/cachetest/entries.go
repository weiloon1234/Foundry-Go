package cachetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type EntryAdapter interface {
	TaggedBackend
	cache.EntryBackend
	cache.TaggedEntryBackend
	cache.BatchBackend
	cache.TaggedBatchBackend
}
type EntryFixture struct {
	Backend EntryAdapter
	Key     func(string) cache.EntryKey
	Track   func(cache.EntryKey)
	// CorruptTagged replaces one exact owned payload with an invalid envelope.
	CorruptTagged func(cache.EntryKey)
}

func (f EntryFixture) snapshot(t *testing.T, key cache.EntryKey) cache.TaggedKey {
	return (TaggedFixture{Backend: f.Backend, Track: f.Track}).Snapshot(t, key, f.Key("entry-tag"))
}
func canonicalEntries(keys ...cache.EntryKey) []cache.EntryKey {
	slices.SortFunc(keys, func(a, b cache.EntryKey) int { return strings.Compare(a.String(), b.String()) })
	return keys
}
func canonicalTagged(keys ...cache.TaggedKey) []cache.TaggedKey {
	slices.SortFunc(keys, func(a, b cache.TaggedKey) int { return strings.Compare(a.DataKey().String(), b.DataKey().String()) })
	return keys
}
func RunEntries(t *testing.T, setup func(*testing.T) EntryFixture) {
	t.Helper()
	for _, tagged := range []bool{false, true} {
		name := "plain"
		if tagged {
			name = "tagged"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			b := f.Backend
			keys := canonicalEntries(f.Key("first"), f.Key("second"), f.Key("other"))
			snapshots := canonicalTagged(f.snapshot(t, keys[0]), f.snapshot(t, keys[1]), f.snapshot(t, keys[2]))
			put := func(index int, data []byte, ttl cache.TTL) error {
				if tagged {
					return b.PutTagged(t.Context(), snapshots[index], data, ttl)
				}
				return b.Put(t.Context(), keys[index], data, ttl)
			}
			get := func(index int) ([]byte, bool, error) {
				if tagged {
					return b.GetTagged(t.Context(), snapshots[index])
				}
				return b.Get(t.Context(), keys[index])
			}
			exists := func(ctx context.Context, index int) (bool, error) {
				if tagged {
					return b.ExistsTagged(ctx, snapshots[index])
				}
				return b.Exists(ctx, keys[index])
			}
			expire := func(ctx context.Context, index int, ttl cache.TTL) (bool, error) {
				if tagged {
					return b.ExpireTagged(ctx, snapshots[index], ttl)
				}
				return b.Expire(ctx, keys[index], ttl)
			}
			if hit, err := exists(t.Context(), 0); err != nil || hit {
				t.Fatal(hit, err)
			}
			if changed, err := expire(t.Context(), 0, cache.Forever()); err != nil || changed {
				t.Fatal("missing expiry created entry", changed, err)
			}
			for i, data := range [][]byte{nil, []byte("value"), []byte("retained")} {
				if err := put(i, data, cache.For(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			for i := range 3 {
				if hit, err := exists(t.Context(), i); err != nil || !hit {
					t.Fatal("empty/live value missing", hit, err)
				}
				for range 2 {
					if changed, err := expire(t.Context(), i, cache.Forever()); err != nil || !changed {
						t.Fatal("unchanged persistent entry was not accepted", changed, err)
					}
				}
			}
			if data, hit, err := get(1); err != nil || !hit || string(data) != "value" {
				t.Fatal("expiry replaced value", string(data), hit, err)
			}
			if _, err := expire(t.Context(), 1, cache.TTL{}); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := exists(ctx, 1); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := expire(ctx, 1, cache.Forever()); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if tagged {
				for _, bad := range [][]cache.TaggedKey{{snapshots[0], snapshots[0]}, {snapshots[1], snapshots[0]}, {{}}, make([]cache.TaggedKey, cache.MaxBatchEntries+1)} {
					if _, err := b.ForgetManyTagged(t.Context(), bad); !errors.Is(err, fault.Invalid) {
						t.Fatal("invalid tagged batch accepted", err)
					}
				}
				if _, err := b.ForgetManyTagged(ctx, snapshots[:2]); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if count, err := b.ForgetManyTagged(t.Context(), nil); err != nil || count != 0 {
					t.Fatal(count, err)
				}
			} else {
				foreign, err := cache.NewEntryKey(cache.Namespace{Application: "other", Environment: "owned-test"}, "family", "key")
				if err != nil {
					t.Fatal(err)
				}
				for _, bad := range [][]cache.EntryKey{{keys[0], keys[0]}, {keys[1], keys[0]}, {{}}, canonicalEntries(keys[0], foreign), make([]cache.EntryKey, cache.MaxBatchEntries+1)} {
					if _, err := b.ForgetMany(t.Context(), bad); !errors.Is(err, fault.Invalid) {
						t.Fatal("invalid batch accepted", err)
					}
				}
				if _, err := b.ForgetMany(ctx, keys[:2]); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if count, err := b.ForgetMany(t.Context(), nil); err != nil || count != 0 {
					t.Fatal(count, err)
				}
			}
			for i := range 3 {
				if _, hit, err := get(i); err != nil || !hit {
					t.Fatal("rejected batch removed live value", hit, err)
				}
			}
			for _, expected := range []uint64{2, 0} {
				var count uint64
				var err error
				if tagged {
					count, err = b.ForgetManyTagged(t.Context(), snapshots[:2])
				} else {
					count, err = b.ForgetMany(t.Context(), keys[:2])
				}
				if err != nil || count != expected {
					t.Fatal(count, err)
				}
			}
			if data, hit, err := get(2); err != nil || !hit || string(data) != "retained" {
				t.Fatal("unselected value changed", string(data), hit, err)
			}
			for i := range 2 {
				if err := put(i, []byte("contended"), cache.Forever()); err != nil {
					t.Fatal(err)
				}
			}
			var winners atomic.Int32
			var group sync.WaitGroup
			for range 16 {
				group.Go(func() {
					var count uint64
					var err error
					if tagged {
						count, err = b.ForgetManyTagged(t.Context(), snapshots[:2])
					} else {
						count, err = b.ForgetMany(t.Context(), keys[:2])
					}
					if err != nil {
						t.Error(err)
						return
					}
					if count != 0 && count != 2 {
						t.Errorf("batch split between callers: %d", count)
					}
					if count == 2 {
						winners.Add(1)
					}
				})
			}
			group.Wait()
			if winners.Load() != 1 {
				t.Fatal("atomic batch did not have exactly one remover", winners.Load())
			}

		})
	}
	t.Run("stale-snapshot-cannot-expire-or-delete-replacement", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		bases := []cache.EntryKey{f.Key("one"), f.Key("two")}
		old := canonicalTagged(f.snapshot(t, bases[0]), f.snapshot(t, bases[1]))
		for _, key := range old {
			if err := b.PutTagged(t.Context(), key, []byte("old"), cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		if err := b.InvalidateTags(t.Context(), []cache.EntryKey{f.Key("entry-tag")}); err != nil {
			t.Fatal(err)
		}
		fresh := canonicalTagged(f.snapshot(t, bases[0]), f.snapshot(t, bases[1]))
		if _, err := b.ExistsTagged(t.Context(), old[0]); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.ExpireTagged(t.Context(), old[0], cache.For(time.Nanosecond)); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.ForgetManyTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := b.ForgetManyTagged(t.Context(), canonicalTagged(old[0], fresh[1])); !errors.Is(err, fault.Invalid) {
			t.Fatal("mixed snapshot accepted", err)
		}
		if count, err := b.ForgetManyTagged(t.Context(), fresh); err != nil || count != 0 {
			t.Fatal("stale physical entries counted as live", count, err)
		}
		for _, key := range fresh {
			if err := b.PutTagged(t.Context(), key, []byte("replacement"), cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.ForgetManyTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		for _, key := range fresh {
			if data, hit, err := b.GetTagged(t.Context(), key); err != nil || !hit || string(data) != "replacement" {
				t.Fatal(string(data), hit, err)
			}
		}
	})
	t.Run("later-corruption-preserves-earlier-entry", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		keys := canonicalTagged(f.snapshot(t, f.Key("first")), f.snapshot(t, f.Key("last")))
		for _, key := range keys {
			if err := b.PutTagged(t.Context(), key, []byte("live"), cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		f.CorruptTagged(keys[1].DataKey())
		if count, err := b.ForgetManyTagged(t.Context(), keys); count != 0 || !errors.Is(err, fault.Invalid) {
			t.Fatal("partial deletion accepted", count, err)
		}
		if data, hit, err := b.GetTagged(t.Context(), keys[0]); err != nil || !hit || string(data) != "live" {
			t.Fatal("earlier entry removed", string(data), hit, err)
		}
		if _, err := b.ExistsTagged(t.Context(), keys[1]); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := b.ExpireTagged(t.Context(), keys[1], cache.Forever()); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	})
	t.Run("maximum-batch-with-full-tag-snapshot", func(t *testing.T) {
		f := setup(t)
		b := f.Backend
		tags := make([]cache.EntryKey, 0, cache.MaxTags+1)
		for i := range cache.MaxTags {
			tags = append(tags, f.Key(fmt.Sprintf("max-tag-%d", i)))
		}
		scope, err := cache.NewNamespaceTagKey(tags[0].Namespace())
		if err != nil {
			t.Fatal(err)
		}
		f.Track(scope)
		tags = append(tags, scope)
		canonicalEntries(tags...)
		versions, err := b.ResolveTags(t.Context(), tags)
		if err != nil {
			t.Fatal(err)
		}
		stamps := make([]cache.TagStamp, len(tags))
		for i, key := range tags {
			stamps[i] = cache.TagStamp{Key: key, Version: versions[i]}
		}
		keys := make([]cache.TaggedKey, cache.MaxBatchEntries)
		for i := range keys {
			keys[i], err = cache.NewTaggedKey(f.Key(fmt.Sprintf("max-value-%d", i)), stamps)
			if err != nil {
				t.Fatal(err)
			}
			f.Track(keys[i].DataKey())
			if err := b.PutTagged(t.Context(), keys[i], []byte("value"), cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		canonicalTagged(keys...)
		if count, err := b.ForgetManyTagged(t.Context(), keys); err != nil || count != cache.MaxBatchEntries {
			t.Fatal(count, err)
		}
		after, err := b.ResolveTags(t.Context(), tags)
		if err != nil || !slices.Equal(after, versions) {
			t.Fatal("batch removed control metadata", err)
		}
	})

}
