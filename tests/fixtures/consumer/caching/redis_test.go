package caching_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/mutatorqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func TestRedisConsumerBuildIsPureAndPreservesTypedProfileAPI(t *testing.T) {
	config := redis.DefaultConfig()
	config.Host = "127.0.0.1"
	config.TLS = redis.DisableTLS
	app, err := foundry.New().Register(caching.RedisModule(config)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), caching.RedisConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		if err := client.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	profiles, err := caching.BindRedis(client, cache.Namespace{Application: "consumer", Environment: "test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "raw@example.test"}
	if err := caching.SaveProfile(t.Context(), profiles, member); !errors.Is(err, fault.Conflict) {
		t.Fatal("unstarted Redis was used", err)
	}
	store, err := caching.RedisStore(client, cache.Namespace{Application: "consumer", Environment: "tagged-test"})
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
	view, err := caching.ForMember(profiles, tags, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := caching.SaveProfile(t.Context(), view, member); !errors.Is(err, fault.Conflict) {
		t.Fatal("tagged view used unstarted client", err)
	}

	if stats := client.Stats(); stats.Open != 0 || stats.Operations != 0 || stats.Ready {
		t.Fatal("construction acquired connections", stats)
	}
}
