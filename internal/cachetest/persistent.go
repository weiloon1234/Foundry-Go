package cachetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

// PersistentBackend is a durable adapter that reclaims expiry itself and
// invalidates a namespace by physical removal.
type PersistentBackend interface {
	BasicEntryBackend
	cache.FlushBackend
	Sweep(context.Context, int) (cacheatomic.PruneResult, error)
}

// PersistentFixture opens backends over one shared durable store. Every opened
// backend reads expiry from Clock; Open varies only the entry and value bounds.
type PersistentFixture struct {
	Open  func(t *testing.T, maxEntries, maxValueBytes int) PersistentBackend
	Clock *testkit.Clock
	Key   func(namespace cache.Namespace, logical string) cache.EntryKey
}

// RunPersistent checks the file and PostgreSQL capacity, unusable-entry and
// namespace flush contracts.
func RunPersistent(t *testing.T, setup func(*testing.T) PersistentFixture) {
	t.Helper()
	namespace := cache.Namespace{Application: "persistent-contract", Environment: "test"}
	t.Run("capacity-counts-only-live-entries", func(t *testing.T) {
		f := setup(t)
		b := f.Open(t, 2, 1024)
		for _, name := range []string{"first", "second"} {
			if err := b.Put(t.Context(), f.Key(namespace, name), []byte(name), cache.For(time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		f.Clock.Advance(2 * time.Second)
		// Expired entries never block a write: it reclaims them first.
		if err := b.Put(t.Context(), f.Key(namespace, "third"), []byte("third"), cache.Forever()); err != nil {
			t.Fatal("expired entries blocked a write", err)
		}
		if _, err := b.Increment(t.Context(), f.Key(namespace, "count"), 1, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(t.Context(), f.Key(namespace, "fourth"), []byte("fourth"), cache.Forever()); !errors.Is(err, fault.Invalid) {
			t.Fatal("live capacity exceeded", err)
		}
		if added, err := b.Add(t.Context(), f.Key(namespace, "third"), []byte("other"), cache.Forever()); err != nil || added {
			t.Fatal("full cache rejected a no-op add", added, err)
		}
		if err := b.Put(t.Context(), f.Key(namespace, "third"), []byte("replaced"), cache.Forever()); err != nil {
			t.Fatal("full cache rejected a replacement", err)
		}
		if data, hit, err := b.Get(t.Context(), f.Key(namespace, "third")); err != nil || !hit || string(data) != "replaced" {
			t.Fatal(string(data), hit, err)
		}
		result, err := b.Sweep(t.Context(), 1)
		if err != nil || result.Removed != 0 || result.Entries != 2 || !result.Complete {
			t.Fatalf("%+v %v", result, err)
		}
		if _, err := b.Sweep(t.Context(), cacheatomic.MaxPrune+1); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	})
	t.Run("sweep-removes-a-bounded-batch", func(t *testing.T) {
		f := setup(t)
		b := f.Open(t, 16, 1024)
		for _, name := range []string{"a", "b", "c"} {
			if err := b.Put(t.Context(), f.Key(namespace, name), nil, cache.For(time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		if err := b.Put(t.Context(), f.Key(namespace, "live"), nil, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		f.Clock.Advance(2 * time.Second)
		first, err := b.Sweep(t.Context(), 2)
		if err != nil || first.Removed != 2 || first.Entries != 2 {
			t.Fatalf("%+v %v", first, err)
		}
		second, err := b.Sweep(t.Context(), 2)
		if err != nil || second.Removed != 1 || second.Entries != 1 || !second.Complete {
			t.Fatalf("%+v %v", second, err)
		}
	})
	t.Run("over-bound-entry-is-a-removable-miss", func(t *testing.T) {
		f := setup(t)
		large := f.Open(t, 16, 1024)
		small := f.Open(t, 16, 64)
		k := f.Key(namespace, "large")
		payload := []byte(strings.Repeat("x", 512))
		if err := large.Put(t.Context(), k, payload, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if _, hit, err := small.Get(t.Context(), k); err != nil || hit {
			t.Fatal("over-bound entry read", hit, err)
		}
		if found, err := small.Exists(t.Context(), k); err != nil || found {
			t.Fatal(found, err)
		}
		if changed, err := small.Expire(t.Context(), k, cache.For(time.Minute)); err != nil || changed {
			t.Fatal(changed, err)
		}
		if added, err := small.Add(t.Context(), k, []byte("small"), cache.Forever()); err != nil || !added {
			t.Fatal("over-bound entry blocked add", added, err)
		}
		if data, hit, err := large.Get(t.Context(), k); err != nil || !hit || string(data) != "small" {
			t.Fatal(string(data), hit, err)
		}
		if err := large.Put(t.Context(), k, payload, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if value, err := small.Increment(t.Context(), k, 2, cache.Forever()); err != nil || value != 2 {
			t.Fatal("over-bound entry blocked increment", value, err)
		}
		if err := large.Put(t.Context(), k, payload, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if removed, err := small.Forget(t.Context(), k); err != nil || removed {
			t.Fatal("over-bound entry reported as live", removed, err)
		}
		if _, hit, err := large.Get(t.Context(), k); err != nil || hit {
			t.Fatal("over-bound entry was not removed", hit, err)
		}
	})
	t.Run("flush-removes-only-its-namespace", func(t *testing.T) {
		f := setup(t)
		b := f.Open(t, 16, 1024)
		foreign := namespace
		foreign.Environment += "-other"
		if err := b.Put(t.Context(), f.Key(namespace, "value"), []byte("value"), cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(t.Context(), f.Key(namespace, "expiring"), []byte("value"), cache.For(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Increment(t.Context(), f.Key(namespace, "count"), 1, cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(t.Context(), f.Key(foreign, "value"), []byte("kept"), cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if removed, err := b.FlushNamespace(t.Context(), namespace); err != nil || removed != 3 {
			t.Fatal(removed, err)
		}
		for _, name := range []string{"value", "expiring", "count"} {
			if found, err := b.Exists(t.Context(), f.Key(namespace, name)); err != nil || found {
				t.Fatal("flushed entry survived", name, found, err)
			}
		}
		if data, hit, err := b.Get(t.Context(), f.Key(foreign, "value")); err != nil || !hit || string(data) != "kept" {
			t.Fatal("foreign namespace changed", string(data), hit, err)
		}
		// A typed store invalidates through the same physical flush.
		store, err := cache.NewStore(b, cache.DefaultConfig(foreign))
		if err != nil {
			t.Fatal(err)
		}
		values := cache.Define("values", cache.StringKeys[string](), cache.JSON[string]())
		handle, err := values.Bind(store)
		if err != nil {
			t.Fatal(err)
		}
		if err := handle.Put(t.Context(), "typed", "before", cache.Forever()); err != nil {
			t.Fatal(err)
		}
		if err := store.Invalidate(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, hit, err := handle.Get(t.Context(), "typed"); err != nil || hit {
			t.Fatal("typed entry survived invalidation", hit, err)
		}
		if found, err := b.Exists(t.Context(), f.Key(foreign, "value")); err != nil || found {
			t.Fatal("namespace entry survived invalidation", found, err)
		}
		if removed, err := b.FlushNamespace(t.Context(), cache.Namespace{}); !errors.Is(err, fault.Invalid) || removed != 0 {
			t.Fatal("invalid namespace flushed", removed, err)
		}
	})
}
