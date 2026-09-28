package pubsub_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
)

func TestInvalidBoundsAndZeroHandles(t *testing.T) {
	backend, err := memory.New(1)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	namespace := keyspace.Namespace{Application: "test", Environment: "validation"}
	for _, change := range []func(*pubsub.Config){func(c *pubsub.Config) { c.Namespace.Application = "" }, func(c *pubsub.Config) { c.MaxKeyBytes = 0 }, func(c *pubsub.Config) { c.MaxConcurrent = 0 }, func(c *pubsub.Config) { c.MaxSubscriptions = 0 }, func(c *pubsub.Config) { c.Timeout = 0 }, func(c *pubsub.Config) { c.Buffer.Messages = 0 }, func(c *pubsub.Config) { c.Buffer.Bytes = 1 }, func(c *pubsub.Config) { c.Buffer.Channels = 0 }} {
		config := pubsub.DefaultConfig(namespace)
		change(&config)
		if _, err := pubsub.NewBroker(backend, config); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	var topic pubsub.Topic[string, payload]
	if _, err := topic.Publish(t.Context(), "key", payload{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var subscription *pubsub.Subscription[payload]
	if _, err := subscription.Receive(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := subscription.Close(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := pubsub.NewChannel(namespace, "events", 0, "key"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

type recursiveBackend struct {
	pubsub.Backend
	broker *pubsub.Broker
}

func (b *recursiveBackend) Publish(ctx context.Context, _ pubsub.Channel, _ []byte) (uint64, error) {
	return 0, b.broker.Close(ctx)
}
func TestBackendCannotWaitForItsOwnBroker(t *testing.T) {
	backend, err := memory.New(1)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	wrapper := &recursiveBackend{Backend: backend}
	broker, err := pubsub.NewBroker(wrapper, pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "cycle"}))
	if err != nil {
		t.Fatal(err)
	}
	wrapper.broker = broker
	t.Cleanup(func() { broker.Close(context.Background()) })
	topic, err := changed.Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := topic.Publish(t.Context(), 1, payload{Text: "cycle", Labels: []string{}}); !errors.Is(err, fault.Cycle) {
		t.Fatal(err)
	}
	if broker.Stats().Closing || broker.Stats().Operations != 0 {
		t.Fatal(broker.Stats())
	}
}
