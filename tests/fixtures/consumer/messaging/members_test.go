package messaging_test

import (
	"context"
	"testing"
	"time"

	"foundry.test/consumer/caching"
	"foundry.test/consumer/messaging"
	"foundry.test/consumer/mutatorqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func TestTypedMemberPublicationUsesSelectedGetter(t *testing.T) {
	backend, err := memory.New(4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	broker, err := pubsub.NewBroker(backend, pubsub.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "pubsub"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { broker.Close(context.Background()) })
	topic, err := messaging.Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "person@example.test"}
	sub, err := messaging.SubscribeMember(t.Context(), topic, id)
	if err != nil {
		t.Fatal(err)
	}
	count, err := messaging.PublishMember(t.Context(), topic, member)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	got, err := messaging.ReceiveMember(ctx, sub)
	want, wantErr := member.AccessEmail()
	if err != nil || wantErr != nil || got.Email != want {
		t.Fatal(got, err, wantErr)
	}
	if err := sub.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestRedisBrokerAssemblyIsPure(t *testing.T) {
	config := redis.DefaultConfig()
	config.Host = "127.0.0.1"
	config.TLS = redis.DisableTLS
	connection := caching.RedisModule(config)
	module := messaging.RedisModule(pubsub.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "pubsub"}), connection.Name)
	app, err := foundry.New().Register(module, connection).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	broker, err := foundation.Resolve(app.Services(), messaging.Broker)
	if err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), caching.RedisConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		broker.Close(context.Background())
		client.Close(context.Background())
		app.Shutdown(context.Background())
	})
	if _, err := messaging.Bind(broker); err != nil {
		t.Fatal(err)
	}
	if broker.Stats().Subscriptions != 0 || client.Stats().Open != 0 || client.Stats().SubscriptionConnections != 0 || client.Stats().Ready {
		t.Fatal("pure assembly started I/O")
	}
}
