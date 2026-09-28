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

func TestTypedMemberInvalidationRefreshesGetterSnapshot(t *testing.T) {
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
	store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "tag-consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := caching.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := caching.BindMemberTags(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	view, err := caching.ForMember(profiles, tags, id)
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "stored@example.test"}
	load := func(context.Context, model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error) {
		return member, nil
	}
	old, err := caching.RememberProfile(t.Context(), view, id, load)
	if err != nil {
		t.Fatal(err)
	}
	member.Email = "new@example.test"
	if err := caching.InvalidateMember(t.Context(), tags, id); err != nil {
		t.Fatal(err)
	}
	fresh, err := caching.RememberProfile(t.Context(), view, id, load)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := member.AccessEmail()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Email != expected || fresh.Email == old.Email || member.Email != "new@example.test" {
		t.Fatal("invalidation/getter semantics failed", old, fresh)
	}
	if err := caching.InvalidateSnapshots(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := view.Get(t.Context(), id); err != nil || hit {
		t.Fatal("tagged snapshot survived namespace invalidation", hit, err)
	}

}
