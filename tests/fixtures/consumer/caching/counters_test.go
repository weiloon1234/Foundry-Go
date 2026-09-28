package caching_test

import (
	"context"
	"math"
	"testing"
	"time"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestModelOwnedActivityCountsPreservePrecisionAndInitialExpiry(t *testing.T) {
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
	store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "consumer-counts", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	counts, err := caching.BindViewCounts(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	if value, err := caching.RecordProfileView(t.Context(), counts, id); err != nil || value != 1 {
		t.Fatal(value, err)
	}
	clock.Advance(23 * time.Hour)
	if value, err := caching.RecordProfileView(t.Context(), counts, id); err != nil || value != 2 {
		t.Fatal(value, err)
	}
	if value, err := caching.UndoProfileView(t.Context(), counts, id); err != nil || value != 1 {
		t.Fatal(value, err)
	}
	clock.Advance(time.Hour)
	if _, found, err := counts.Get(t.Context(), id); err != nil || found {
		t.Fatal("later view refreshed window", found, err)
	}
	if added, err := counts.Add(t.Context(), id, math.MaxInt64-1, cache.Forever()); err != nil || !added {
		t.Fatal(added, err)
	}
	if value, err := caching.RecordProfileView(t.Context(), counts, id); err != nil || value != math.MaxInt64 {
		t.Fatal("large counter lost precision", value, err)
	}
}
