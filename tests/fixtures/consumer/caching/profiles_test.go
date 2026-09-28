package caching_test

import (
	"context"
	"testing"
	"time"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestTypedProfileSnapshotUsesGetterAndPreservesStoredMember(t *testing.T) {
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
	store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "consumer", Environment: "test"}))
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
	member := mutatorqueries.Member{ID: id, Email: "stored@example.test"}
	expected, err := member.AccessEmail()
	if err != nil {
		t.Fatal(err)
	}
	if err := caching.SaveProfile(t.Context(), profiles, member); err != nil {
		t.Fatal(err)
	}
	got, found, err := caching.ReadProfile(t.Context(), profiles, id)
	if err != nil || !found || got.Email != expected || member.Email != "stored@example.test" {
		t.Fatal("cache lost accessor/identity semantics", got, found, err)
	}
	got.Labels[0] = "changed"
	again, found, err := caching.ReadProfile(t.Context(), profiles, id)
	if err != nil || !found || again.Labels[0] != "active" {
		t.Fatal("cache snapshot aliases caller data")
	}
	clock.Advance(5 * time.Minute)
	if _, found, err := profiles.Get(t.Context(), id); err != nil || found {
		t.Fatal("profile TTL ignored", found, err)
	}
	loads := 0
	load := func(ctx context.Context, key model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error) {
		loads++
		if key != id {
			t.Fatal("wrong model key")
		}
		if err := ctx.Err(); err != nil {
			return mutatorqueries.Member{}, err
		}
		return member, nil
	}
	for range 2 {
		got, err := caching.RememberProfile(t.Context(), profiles, id, load)
		if err != nil || got.Email != expected || member.Email != "stored@example.test" {
			t.Fatal(got, err)
		}
	}
	if loads != 1 {
		t.Fatal("cache hit ran model loader", loads)
	}
	member.Email = "changed@example.test"
	if err := caching.InvalidateSnapshots(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	refreshed, err := caching.RememberProfile(t.Context(), profiles, id, load)
	expected, getterErr := member.AccessEmail()
	if err != nil || getterErr != nil || loads != 2 || refreshed.Email != expected || member.Email != "changed@example.test" {
		t.Fatal("namespace invalidation lost typed getter semantics", refreshed, err, getterErr, loads)
	}

}
