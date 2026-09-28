package coordination_test

import (
	"context"
	"testing"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/coordination"
	"foundry.test/consumer/mutatorqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func TestTypedMemberWorkUsesFrameworkOwnership(t *testing.T) {
	backend, err := memory.New(4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	manager, err := lease.NewManager(backend, lease.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	locks, err := coordination.Bind(manager)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ran, err := coordination.RefreshMember(t.Context(), locks, id, func(ctx context.Context, got model.ID[mutatorqueries.Member]) error {
		calls++
		if got != id {
			t.Fatal("model key changed")
		}
		if _, ok, err := coordination.TryRefresh(ctx, locks, id); err != nil || ok {
			t.Fatal(ok, err)
		}
		return nil
	})
	if !ran || err != nil || calls != 1 {
		t.Fatal(ran, err, calls)
	}
	guard, ok, err := coordination.TryRefresh(t.Context(), locks, id)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := guard.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestRedisLeaseAssemblyIsPure(t *testing.T) {
	config := redis.DefaultConfig()
	config.Host = "127.0.0.1"
	config.TLS = redis.DisableTLS
	connection := caching.RedisModule(config)
	leases := coordination.RedisModule(lease.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "test"}), connection.Name)
	app, err := foundry.New().Register(leases, connection).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := foundation.Resolve(app.Services(), coordination.Manager)
	if err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), caching.RedisConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		manager.Close(context.Background())
		client.Close(context.Background())
		app.Shutdown(context.Background())
	})
	if _, err := coordination.Bind(manager); err != nil {
		t.Fatal(err)
	}
	if manager.Stats().Active != 0 || client.Stats().Open != 0 || client.Stats().Ready {
		t.Fatal("construction started infrastructure")
	}
}
