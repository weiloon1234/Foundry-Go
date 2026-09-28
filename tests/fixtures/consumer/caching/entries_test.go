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

func TestTypedEntryOperationsKeepGetterSnapshotsAndModelKeys(t *testing.T) {
	clock := testkit.NewClock(time.Now())
	backend, err := memory.New(memory.DefaultConfig(), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close(context.Background()) })
	store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "consumer-entry", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := caching.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	second, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	members := []mutatorqueries.Member{{ID: first, Email: "first@example.test"}, {ID: second, Email: "second@example.test"}}
	for _, member := range members {
		if err := caching.SaveProfile(t.Context(), profiles, member); err != nil {
			t.Fatal(err)
		}
	}
	if exists, err := caching.ProfileExists(t.Context(), profiles, first); err != nil || !exists {
		t.Fatal(exists, err)
	}
	if changed, err := caching.RefreshProfileExpiry(t.Context(), profiles, first, cache.For(time.Hour)); err != nil || !changed {
		t.Fatal(changed, err)
	}
	clock.Advance(6 * time.Minute)
	firstValue, hit, err := caching.ReadProfile(t.Context(), profiles, first)
	expected, getterErr := members[0].AccessEmail()
	if err != nil || getterErr != nil || !hit || firstValue.Email != expected || members[0].Email != "first@example.test" {
		t.Fatal("expiry replaced getter snapshot", firstValue, hit, err, getterErr)
	}
	if exists, err := caching.ProfileExists(t.Context(), profiles, second); err != nil || exists {
		t.Fatal(exists, err)
	}
	if count, err := caching.ForgetProfiles(t.Context(), profiles, second, first, first); err != nil || count != 1 {
		t.Fatal("batch count includes duplicate/expired entries", count, err)
	}
	for _, member := range members {
		if exists, err := caching.ProfileExists(t.Context(), profiles, member.ID); err != nil || exists {
			t.Fatal(exists, err)
		}
	}
	tags, err := caching.BindMemberTags(store)
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := caching.ForMember(profiles, tags, first)
	if err != nil {
		t.Fatal(err)
	}
	if err := caching.SaveProfile(t.Context(), tagged, members[0]); err != nil {
		t.Fatal(err)
	}
	if changed, err := caching.RefreshProfileExpiry(t.Context(), tagged, first, cache.Forever()); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if err := store.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if exists, err := caching.ProfileExists(t.Context(), tagged, first); err != nil || exists {
		t.Fatal("invalidated entry exists", exists, err)
	}
}
