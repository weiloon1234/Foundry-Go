package cachetest

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// NamespaceFixture owns exact test addresses; no backend enumeration is needed.
type NamespaceFixture struct {
	Backend   TaggedBackend
	Namespace cache.Namespace
	Track     func(cache.EntryKey)
}

func (f NamespaceFixture) store(t *testing.T, namespace cache.Namespace) *cache.Store {
	t.Helper()
	store, err := cache.NewStore(f.Backend, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func (f NamespaceFixture) snapshot(t *testing.T, namespace cache.Namespace, name cache.Name, logical string, tags ...cache.EntryKey) cache.TaggedKey {
	t.Helper()
	base, err := cache.NewEntryKey(namespace, name, logical)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := cache.NewNamespaceTagKey(namespace)
	if err != nil {
		t.Fatal(err)
	}
	f.Track(scope)
	for _, tag := range tags {
		f.Track(tag)
	}
	return (TaggedFixture{Backend: f.Backend, Track: f.Track}).Snapshot(t, base, append(tags, scope)...)
}

// RunNamespace checks the same automatic typed-store contract in memory and Redis.
func RunNamespace(t *testing.T, setup func(*testing.T) NamespaceFixture) {
	t.Helper()
	t.Run("values-views-counters-and-scope", func(t *testing.T) {
		f := setup(t)
		first := f.store(t, f.Namespace)
		second := f.store(t, f.Namespace)
		foreign := f.Namespace
		foreign.Environment += "-other"
		other := f.store(t, foreign)
		values := cache.Define("namespace-values", cache.StringKeys[string](), cache.JSON[string]())
		counts := cache.DefineCounter("namespace-counts", cache.StringKeys[string]())
		tags := cache.DefineTag("namespace-tags", cache.StringKeys[string]())
		one, err := values.Bind(first)
		if err != nil {
			t.Fatal(err)
		}
		two, err := values.Bind(second)
		if err != nil {
			t.Fatal(err)
		}
		retained, err := values.Bind(other)
		if err != nil {
			t.Fatal(err)
		}
		tag, err := tags.Bind(first)
		if err != nil {
			t.Fatal(err)
		}
		view, err := one.WithTags(tag.For("group"))
		if err != nil {
			t.Fatal(err)
		}
		counter, err := counts.Bind(first)
		if err != nil {
			t.Fatal(err)
		}
		taggedCounter, err := counter.WithTags(tag.For("group"))
		if err != nil {
			t.Fatal(err)
		}
		tagKey, err := cache.NewEntryKey(f.Namespace, tags.Name(), "group")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []cache.Name{values.Name(), counts.Name()} {
			f.snapshot(t, f.Namespace, name, "key")
			f.snapshot(t, f.Namespace, name, "key", tagKey)
		}
		f.snapshot(t, foreign, values.Name(), "key")
		versions, err := f.Backend.ResolveTags(t.Context(), []cache.EntryKey{tagKey})
		if err != nil {
			t.Fatal(err)
		}
		for _, handle := range []cache.Cache[string, string]{one, view, retained} {
			if err := handle.Put(t.Context(), "key", "before", cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		for _, handle := range []cache.Counter[string]{counter, taggedCounter} {
			if _, err := handle.Increment(t.Context(), "key", 42, cache.Forever()); err != nil {
				t.Fatal(err)
			}
		}
		if err := second.Invalidate(t.Context()); err != nil {
			t.Fatal(err)
		}
		for _, handle := range []cache.Cache[string, string]{one, two, view} {
			if _, hit, err := handle.Get(t.Context(), "key"); err != nil || hit {
				t.Fatal("namespace value survived", hit, err)
			}
			if added, err := handle.Add(t.Context(), "key", "new", cache.Forever()); err != nil || !added {
				t.Fatal(added, err)
			}
			if _, err := handle.Forget(t.Context(), "key"); err != nil {
				t.Fatal(err)
			}
		}
		for _, handle := range []cache.Counter[string]{counter, taggedCounter} {
			if value, err := handle.Increment(t.Context(), "key", 3, cache.Forever()); err != nil || value != 3 {
				t.Fatal("counter generation survived", value, err)
			}
		}
		if value, hit, err := retained.Get(t.Context(), "key"); err != nil || !hit || value != "before" {
			t.Fatal("foreign namespace changed", value, hit, err)
		}
		after, err := f.Backend.ResolveTags(t.Context(), []cache.EntryKey{tagKey})
		if err != nil || after[0] != versions[0] {
			t.Fatal("application tag metadata rotated", after, err)
		}
	})
	t.Run("metadata-loss-and-stable-forever-address", func(t *testing.T) {
		f := setup(t)
		store := f.store(t, f.Namespace)
		values := cache.Define("generation", cache.StringKeys[string](), cache.JSON[string]())
		handle, err := values.Bind(store)
		if err != nil {
			t.Fatal(err)
		}
		old := f.snapshot(t, f.Namespace, values.Name(), "v1")
		scope, err := cache.NewNamespaceTagKey(f.Namespace)
		if err != nil {
			t.Fatal(err)
		}
		ordinary, err := cache.NewEntryKey(f.Namespace, "generation", "v1")
		if err != nil {
			t.Fatal(err)
		}
		if ordinary.String() == scope.String() || old.DataKey() == scope {
			t.Fatal("reserved namespace metadata collided")
		}
		if err := handle.Put(t.Context(), "v1", "stale", cache.Forever()); err != nil {
			t.Fatal(err)
		}
		// Delete only this fixture's exact control key to simulate eviction.
		if removed, err := f.Backend.Forget(t.Context(), scope); err != nil || !removed {
			t.Fatal(removed, err)
		}
		if _, hit, err := handle.Get(t.Context(), "v1"); err != nil || hit {
			t.Fatal("metadata loss resurrected data", hit, err)
		}
		for range 32 {
			fresh := f.snapshot(t, f.Namespace, values.Name(), "v1")
			if fresh.DataKey() != old.DataKey() || fresh.FillKey() == old.FillKey() {
				t.Fatal("namespace identity mismatch")
			}
			if err := handle.Put(t.Context(), "v1", "current", cache.Forever()); err != nil {
				t.Fatal(err)
			}
			if err := f.Backend.PutTagged(t.Context(), old, []byte("stale"), cache.Forever()); !errors.Is(err, fault.Conflict) {
				t.Fatal("stale snapshot wrote", err)
			}
			if _, err := f.Backend.ForgetTagged(t.Context(), old); !errors.Is(err, fault.Conflict) {
				t.Fatal("stale snapshot removed replacement", err)
			}
			if value, hit, err := handle.Get(t.Context(), "v1"); err != nil || !hit || value != "current" {
				t.Fatal(value, hit, err)
			}
			if err := store.Invalidate(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("remember-owner-cannot-publish-after-rotation", func(t *testing.T) {
		f := setup(t)
		store := f.store(t, f.Namespace)
		values := cache.Define("namespace-slow", cache.StringKeys[string](), cache.JSON[string]())
		handle, err := values.Bind(store)
		if err != nil {
			t.Fatal(err)
		}
		f.snapshot(t, f.Namespace, values.Name(), "key")
		ctx, cancel := context.WithCancel(t.Context())
		started := make(chan struct{})
		release := make(chan struct{})
		result := make(chan error, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err := handle.Remember(ctx, "key", cache.Forever(), func(ctx context.Context) (string, error) {
				close(started)
				select {
				case <-release:
					return "old", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			})
			result <- err
		}()
		t.Cleanup(func() { cancel(); <-done })
		select {
		case <-started:
		case <-done:
			t.Fatal("loader did not start", <-result)
		}
		if err := store.Invalidate(t.Context()); err != nil {
			t.Fatal(err)
		}
		if value, err := handle.Remember(t.Context(), "key", cache.Forever(), func(context.Context) (string, error) { return "new", nil }); err != nil || value != "new" {
			t.Fatal("joined old fill", value, err)
		}
		close(release)
		// The old caller keeps its loaded value; its stale publication is
		// rejected (and reported) instead of failing the request.
		if err := <-result; err != nil {
			t.Fatal("old fill failed its caller", err)
		}
		if value, hit, err := handle.Get(t.Context(), "key"); err != nil || !hit || value != "new" {
			t.Fatal(value, hit, err)
		}
	})
}
