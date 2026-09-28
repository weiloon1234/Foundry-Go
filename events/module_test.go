package events_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestModuleConstructsListenersOnceWithTheirOwnBusAndIsolatesApplications(t *testing.T) {
	key := foundation.NewKey[*events.Bus]("events")
	topic := events.Define[int]("test.module", 1)
	var constructed, called atomic.Int64
	module := events.Module("events", key, events.DefaultConfig())
	provider := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{"events"}, OnRegister: func(r *foundation.Registrar) error {
		return events.RegisterListener(r, key, topic, "domain.handler", func(resolver foundation.Resolver) (events.Handler[int], error) {
			bus, err := foundation.Resolve(resolver, key)
			if err != nil {
				return nil, err
			}
			if err := topic.Dispatch(t.Context(), bus, 7); !errors.Is(err, fault.Closed) {
				return nil, errors.New("listener constructor ran its unstarted bus")
			}
			if err := bus.Start(t.Context()); !errors.Is(err, fault.Invalid) {
				return nil, errors.New("listener constructor started the module-owned bus")
			}
			constructed.Add(1)
			return func(context.Context, int) error { called.Add(1); return nil }, nil
		})
	}}
	var previous *events.Bus
	for range 2 {
		app, err := foundry.New().Register(provider, module).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		bus, err := foundation.Resolve(app.Services(), key)
		if err != nil {
			t.Fatal(err)
		}
		if bus == previous {
			t.Fatal("application reused a previous bus")
		}
		previous = bus
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := topic.Dispatch(t.Context(), bus, 7); err != nil {
			t.Fatal(err)
		}
		if err := app.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-bus.Done():
		default:
			t.Fatal("application shutdown left the bus open")
		}
	}
	if constructed.Load() != 2 || called.Load() != 2 {
		t.Fatal("listener construction or invocation escaped application ownership")
	}
}
