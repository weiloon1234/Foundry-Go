package caching_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func TestDistributedProfilesKeepTypedContractAndRequireAuthority(t *testing.T) {
	config := redis.DefaultConfig()
	config.Host = "127.0.0.1"
	config.TLS = redis.DisableTLS
	client, err := redis.Prepare(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	manager, err := lease.NewManager(client, lease.DefaultConfig(cache.Namespace{Application: "consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close(context.Background()) })
	profiles, err := caching.BindDistributed(manager)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = caching.RememberProfile(t.Context(), profiles, id, func(context.Context, model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error) {
		called = true
		return mutatorqueries.Member{ID: id, Email: "raw@example.test"}, nil
	})
	if !errors.Is(err, fault.Conflict) || called {
		t.Fatal("unavailable authority caused fallback", err, called)
	}
	store, err := caching.DistributedStore(manager)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err = caching.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := caching.BindMemberTags(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caching.ForMember(profiles, tags, id); err != nil {
		t.Fatal(err)
	}
	if manager.Stats().Active != 0 || client.Stats().Open != 0 {
		t.Fatal("construction started resources")
	}
}
