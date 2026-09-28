package pubsub_test

import (
	"context"
	"errors"
	"testing"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/pubsub/memory"
)

func TestModuleDrainsBeforeBorrowedAdapter(t *testing.T) {
	backendKey := foundation.NewKey[*memory.Backend]("test.backend")
	brokerKey := foundation.NewKey[*pubsub.Broker]("test.pubsub")
	closed := false
	adapter := foundation.Module{Name: "test.backend", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, backendKey, func(foundation.Resolver) (*memory.Backend, error) { return memory.New(8) })
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		b, err := foundation.Resolve(r.Services(), backendKey)
		if err != nil {
			return err
		}
		return r.OnShutdown("backend", func(context.Context) error {
			broker, err := foundation.Resolve(r.Services(), brokerKey)
			if err != nil {
				return err
			}
			select {
			case <-broker.Done():
			default:
				return errors.New("adapter closed before pub/sub drained")
			}
			closed = true
			return b.Close()
		})
	}}
	module := pubsub.Module("test.pubsub", brokerKey, pubsub.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "module"}), []foundation.ProviderID{adapter.Name}, func(r foundation.Resolver) (pubsub.Backend, error) { return foundation.Resolve(r, backendKey) })
	app, err := foundry.New().Register(module, adapter).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown(context.Background()) })
	broker, err := foundation.Resolve(app.Services(), brokerKey)
	if err != nil {
		t.Fatal(err)
	}
	if broker.Stats().Operations != 0 || broker.Stats().Subscriptions != 0 {
		t.Fatal("construction started work")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	topic, err := changed.Bind(broker)
	if err != nil {
		t.Fatal(err)
	}
	s, err := topic.Subscribe(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s.Done())
	if !closed {
		t.Fatal("backend cleanup missing")
	}
}
