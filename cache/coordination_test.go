package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/redis"
)

type unfencedTags struct {
	cache.CoordinatedBackend
	cache.TaggedBackend
	lease.Backend
}

func TestCoordinatedStoreValidatesAuthorityAndConstruction(t *testing.T) {
	namespace := cache.Namespace{Application: "test", Environment: "coordination"}
	config := cache.DefaultConfig(namespace)
	options := cache.DefaultCoordinationConfig()
	if _, err := cache.NewCoordinatedStore(nil, config, options); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	local, _ := memory.New(2)
	t.Cleanup(func() { local.Close() })
	missing, err := lease.NewManager(local, lease.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { missing.Close(context.Background()) })
	if _, err := cache.NewCoordinatedStore(missing, config, options); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported adapter fell back", err)
	}
	redisConfig := redis.DefaultConfig()
	redisConfig.Host = "127.0.0.1"
	redisConfig.TLS = redis.DisableTLS
	client, err := redis.Prepare(redisConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	m, err := lease.NewManager(client, lease.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	for range 2 {
		if _, err := cache.NewCoordinatedStore(m, config, options); err != nil {
			t.Fatal("declaration identity was duplicated", err)
		}
	}
	badConfig := config
	badConfig.Namespace.Environment = "other"
	if _, err := cache.NewCoordinatedStore(m, badConfig, options); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, bad := range []cache.CoordinationConfig{{}, {LeaseDuration: time.Second, Wait: -1}, {LeaseDuration: time.Second, Wait: time.Hour}} {
		if _, err := cache.NewCoordinatedStore(m, config, bad); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	limited, err := lease.NewManager(unfencedTags{client, client, client}, lease.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { limited.Close(context.Background()) })
	if _, err := cache.NewCoordinatedStore(limited, config, options); !errors.Is(err, fault.Invalid) {
		t.Fatal("unfenced namespace snapshots were accepted", err)
	}
	if m.Stats().Active != 0 || client.Stats().Open != 0 || client.Stats().Ready {
		t.Fatal("construction acquired resources")
	}
	m.Close(t.Context())
	if _, err := cache.NewCoordinatedStore(m, config, options); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}
