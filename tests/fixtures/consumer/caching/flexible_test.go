package caching_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestFlexibleProfileRefreshesInBackgroundAndMemoizesRequests(t *testing.T) {
	clock := testkit.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	backend, err := memory.New(memory.DefaultConfig(), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "consumer", Environment: "flexible"}))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := caching.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	var email atomic.Value
	email.Store("first@example.test")
	var loads atomic.Int32
	load := func(ctx context.Context, key model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error) {
		loads.Add(1)
		return mutatorqueries.Member{ID: key, Email: email.Load().(string)}, nil
	}
	first, err := caching.FlexibleProfile(t.Context(), profiles, id, load)
	if err != nil || first.Email == "" || loads.Load() != 1 {
		t.Fatal(first, err, loads.Load())
	}
	// After a minute the stale snapshot is served at once and refreshed behind it.
	email.Store("second@example.test")
	clock.Advance(2 * time.Minute)
	stale, err := caching.FlexibleProfile(t.Context(), profiles, id, load)
	if err != nil || stale.Email != first.Email {
		t.Fatal("stale profile was not served immediately", stale, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := caching.FlexibleProfile(t.Context(), profiles, id, load)
		if err != nil {
			t.Fatal(err)
		}
		if current.Email != first.Email {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("profile was not refreshed in the background")
		}
		time.Sleep(time.Millisecond)
	}
	if loads.Load() != 2 {
		t.Fatal("refresh was not coalesced", loads.Load())
	}
	// One request reads a key once, even if another request changes it meanwhile.
	request := cache.WithMemo(t.Context())
	before, found, err := caching.ReadProfile(request, profiles, id)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if _, err := profiles.Forget(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	again, found, err := caching.ReadProfile(request, profiles, id)
	if err != nil || !found || again.Email != before.Email {
		t.Fatal("request memo was not reused", again, found, err)
	}
	if _, found, err := caching.ReadProfile(t.Context(), profiles, id); err != nil || found {
		t.Fatal("forgotten profile was read outside the request memo", found, err)
	}
}
