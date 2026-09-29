package null_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/null"
	"github.com/weiloon1234/Foundry-Go/fault"
)

var (
	values   = cache.Define("values", cache.StringKeys[string](), cache.JSON[string]())
	counters = cache.DefineCounter("counters", cache.StringKeys[string]())
	groups   = cache.DefineTag("groups", cache.StringKeys[string]())
)

func TestNullStoreRetainsNothingButSupportsEveryCapability(t *testing.T) {
	store, err := cache.NewStore(null.New(), cache.DefaultConfig(cache.Namespace{Application: "null", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Require(cache.Requirements{Tags: true, Counters: true, Entries: true, Batches: true}); err != nil {
		t.Fatal("null store rejected a capability", err)
	}
	c, err := values.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := groups.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := c.WithTags(tags.For("team"))
	if err != nil {
		t.Fatal(err)
	}
	for _, handle := range []cache.Cache[string, string]{c, view} {
		if err := handle.Put(t.Context(), "key", "value", cache.For(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, found, err := handle.Get(t.Context(), "key"); err != nil || found {
			t.Fatal("null store returned a value", found, err)
		}
		if added, err := handle.Add(t.Context(), "key", "value", cache.Forever()); err != nil || !added {
			t.Fatal(added, err)
		}
		if found, err := handle.Exists(t.Context(), "key"); err != nil || found {
			t.Fatal(found, err)
		}
		if changed, err := handle.Expire(t.Context(), "key", cache.Forever()); err != nil || changed {
			t.Fatal(changed, err)
		}
		if removed, err := handle.Forget(t.Context(), "key"); err != nil || removed {
			t.Fatal(removed, err)
		}
		if results, err := handle.GetMany(t.Context(), "a", "b"); err != nil || len(results) != 2 || results[0].Found || results[1].Found {
			t.Fatal(results, err)
		}
		if removed, err := handle.ForgetMany(t.Context(), "a", "b"); err != nil || removed != 0 {
			t.Fatal(removed, err)
		}
		calls := 0
		for range 2 {
			value, err := handle.Remember(t.Context(), "remembered", cache.Forever(), func(context.Context) (string, error) { calls++; return "loaded", nil })
			if err != nil || value != "loaded" {
				t.Fatal(value, err)
			}
		}
		if calls != 2 {
			t.Fatal("Remember did not run its loader on every call", calls)
		}
	}
	counter, err := counters.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if value, err := counter.Increment(t.Context(), "attempts", 3, cache.For(time.Minute)); err != nil || value != 3 {
			t.Fatal("counter was retained", value, err)
		}
	}
	if err := store.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.InvalidateTags(t.Context(), tags.For("team")); err != nil {
		t.Fatal(err)
	}
}

func TestNullBackendValidatesInputs(t *testing.T) {
	backend := null.New()
	key, err := cache.NewEntryKey(cache.Namespace{Application: "null", Environment: "test"}, "values", "key")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(t.Context(), cache.EntryKey{}, nil, cache.Forever()); err == nil {
		t.Fatal("invalid key was accepted")
	}
	if err := backend.Put(t.Context(), key, nil, cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid TTL was accepted", err)
	}
	if _, _, err := backend.Get(nil, key); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := backend.Get(canceled, key); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := backend.Increment(t.Context(), key, 1, cache.Forever()); err != nil {
		t.Fatal(err)
	}
}
