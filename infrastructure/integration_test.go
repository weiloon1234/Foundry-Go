package infrastructure_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	redistest "github.com/weiloon1234/Foundry-Go/testkit/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func TestNativeNamedDatabaseAndSharedRedisIsolation(t *testing.T) {
	s := memorySettings()
	s.Namespace.Application = "infra-" + rand.Text()
	c := infrastructure.DefaultConnectionSettings()
	c.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Database.Connections = infrastructure.DatabaseConnections{"default": c, "reports": c}
	r := infrastructure.RedisSettingsFromConfig(redistest.Config(t))
	s.Redis.Connections = infrastructure.RedisConnections{"default": r, "second": r}
	for name, settings := range s.Cache.Stores {
		settings.Driver = infrastructure.RedisCache
		settings.Require.DistributedFills = true
		s.Cache.Stores[name] = settings
	}
	brokerConfig := infrastructure.DefaultBrokerSettings()
	brokerConfig.Driver = infrastructure.RedisBroker
	s.PubSub.Connections = infrastructure.Brokers{"default": brokerConfig, "second": brokerConfig}
	realtimeConfig := infrastructure.DefaultRealtimeConnectionSettings()
	realtimeConfig.Driver = infrastructure.RedisRealtime
	s.Realtime.Connections = infrastructure.RealtimeConnections{"default": realtimeConfig, "second": realtimeConfig}
	s.Coordination.Enabled = true
	s.Coordination.Driver = infrastructure.RedisCoordination
	plan, err := infrastructure.Configure(s)
	if err != nil {
		t.Fatal(err)
	}
	app := built(t, plan)
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	resolved := services(t, app)
	db, _ := resolved.Database()
	alias, _ := resolved.Databases.Connection("default")
	reports, _ := resolved.Databases.Connection("reports")
	if db != alias || db == reports {
		t.Fatal("database ownership invalid")
	}
	for _, connection := range []*database.DB{db, reports} {
		if err := connection.Transaction(t.Context(), func(tx *database.Tx) error { _, err := tx.Exec(t.Context(), "SELECT 1"); return err }); err != nil {
			t.Fatal(err)
		}
	}
	client, _ := resolved.RedisConnection()
	named, _ := foundation.Resolve(app.Services(), infrastructure.RedisKey("default"))
	other, _ := resolved.Redis.Connection("second")
	if client != named || client == other {
		t.Fatal("Redis ownership invalid")
	}
	first, _ := resolved.Cache()
	second, _ := resolved.Caches.Store("second")
	a, _ := values.Bind(first)
	b, _ := values.Bind(second)
	if err := a.Put(t.Context(), "same-key", "first", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := b.Get(t.Context(), "same-key"); err != nil || hit {
		t.Fatal("shared Redis namespaces collided", err)
	}
	got, err := b.Remember(t.Context(), "same-key", cache.Forever(), func(context.Context) (string, error) { return "second", nil })
	if err != nil || got != "second" {
		t.Fatal(got, err)
	}
	broker, err := resolved.Broker()
	if err != nil {
		t.Fatal(err)
	}
	otherBroker, err := resolved.Brokers.Broker("second")
	if err != nil {
		t.Fatal(err)
	}
	declaration := pubsub.Define[string, string]("native", 1, keyspace.StringKeys[string]())
	topic, err := declaration.Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	otherTopic, err := declaration.Bind(otherBroker)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := topic.Subscribe(t.Context(), "room")
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close(context.Background())
	if n, err := otherTopic.Publish(t.Context(), "room", "isolated"); err != nil || n != 0 {
		t.Fatal("Redis brokers collided", err)
	}
	if n, err := topic.Publish(t.Context(), "room", "delivered"); err != nil || n != 1 {
		t.Fatal("Redis configured publication failed", err)
	}
	if _, err := subscription.Receive(t.Context()); err != nil {
		t.Fatal(err)
	}
	channel := websocket.Public[struct{}]("configured", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	event := websocket.RawOutgoing(channel, "message")
	registry, err := websocket.NewRegistry(websocket.Register(channel, event.Registration()))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []websocket.ConnectionName{"default", "second"} {
		connection, err := resolved.Realtime.Connection(name)
		if err != nil {
			t.Fatal(err)
		}
		publisher, err := connection.NewPublisher(registry, websocket.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		id, err := websocket.Publish(t.Context(), publisher, channel, "room", event, json.RawMessage(`{"configured":true}`))
		if err != nil || id.IsZero() {
			t.Fatal("Redis configured realtime publication failed", err)
		}
		if err := publisher.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resolved.Leases(); err != nil {
		t.Fatal(err)
	}
	// Remove only this test's declared entry; retain unrelated backend data.
	if _, err := a.Forget(t.Context(), "same-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Forget(t.Context(), "same-key"); err != nil {
		t.Fatal(err)
	}
}
