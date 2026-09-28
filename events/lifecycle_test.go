package events_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestModuleRejectsListenerConstructionFailuresBeforeBoot(t *testing.T) {
	topic := events.Define[int]("test.construction", 1)
	key := foundation.NewKey[*events.Bus]("events")
	missing := foundation.NewKey[int]("missing")
	sentinel := errors.New("constructor rejected")
	cases := []struct {
		name     string
		factory  func(foundation.Resolver) (events.Handler[int], error)
		expected error
	}{
		{"missing", func(s foundation.Resolver) (events.Handler[int], error) {
			_, err := foundation.Resolve(s, missing)
			return nil, err
		}, fault.Missing},
		{"nil_handler", func(foundation.Resolver) (events.Handler[int], error) { return nil, nil }, fault.Invalid},
		{"panic", func(foundation.Resolver) (events.Handler[int], error) { panic("private constructor data") }, fault.Panicked},
		{"goexit", func(foundation.Resolver) (events.Handler[int], error) { runtime.Goexit(); return nil, nil }, fault.Panicked},
		{"error", func(foundation.Resolver) (events.Handler[int], error) { return nil, sentinel }, sentinel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			booted := false
			provider := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{"events"}, OnRegister: func(r *foundation.Registrar) error {
				return events.RegisterListener(r, key, topic, "handler", tc.factory)
			}, OnBoot: func(context.Context, *foundation.Runtime) error { booted = true; return nil }}
			_, err := foundry.New().Register(events.Module("events", key, events.DefaultConfig()), provider).Build(t.Context())
			if !errors.Is(err, tc.expected) || strings.Contains(err.Error(), "private") || booted {
				t.Fatal("listener construction failure escaped or booted the application", err)
			}
		})
	}
}

func TestModuleRegistrationsRejectDuplicatesAndConflictingPayloadTypes(t *testing.T) {
	key := foundation.NewKey[*events.Bus]("events")
	topic := events.Define[int]("test.conflict", 1)
	for _, conflictingType := range []bool{false, true} {
		provider := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{"events"}, OnRegister: func(r *foundation.Registrar) error {
			if err := events.RegisterListener(r, key, topic, "first", func(foundation.Resolver) (events.Handler[int], error) {
				return func(context.Context, int) error { return nil }, nil
			}); err != nil {
				return err
			}
			if conflictingType {
				return events.RegisterListener(r, key, events.Define[string]("test.conflict", 1), "second", func(foundation.Resolver) (events.Handler[string], error) {
					return func(context.Context, string) error { return nil }, nil
				})
			}
			return events.RegisterListener(r, key, topic, "first", func(foundation.Resolver) (events.Handler[int], error) {
				return func(context.Context, int) error { return nil }, nil
			})
		}}
		if _, err := foundry.New().Register(events.Module("events", key, events.DefaultConfig()), provider).Build(t.Context()); !errors.Is(err, fault.Duplicate) {
			t.Fatal("invalid event registration accepted", err)
		}
	}
}

func TestModuleKeepsIndependentBusRegistries(t *testing.T) {
	firstKey, secondKey := foundation.NewKey[*events.Bus]("first"), foundation.NewKey[*events.Bus]("second")
	first, second := events.Define[int]("test.shared_name", 1), events.Define[string]("test.shared_name", 1)
	var firstCalls, secondCalls atomic.Int64
	provider := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{"first", "second"}, OnRegister: func(r *foundation.Registrar) error {
		if err := events.RegisterListener(r, firstKey, first, "record", func(foundation.Resolver) (events.Handler[int], error) {
			return func(context.Context, int) error { firstCalls.Add(1); return nil }, nil
		}); err != nil {
			return err
		}
		return events.RegisterListener(r, secondKey, second, "record", func(foundation.Resolver) (events.Handler[string], error) {
			return func(context.Context, string) error { secondCalls.Add(1); return nil }, nil
		})
	}}
	app := testkit.Start(t, foundry.New().Register(provider, events.Module("first", firstKey, events.DefaultConfig()), events.Module("second", secondKey, events.DefaultConfig())))
	one, err := foundation.Resolve(app.Services(), firstKey)
	if err != nil {
		t.Fatal(err)
	}
	two, err := foundation.Resolve(app.Services(), secondKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Dispatch(t.Context(), one, 7); err != nil {
		t.Fatal(err)
	}
	if err := second.Dispatch(t.Context(), two, "typed"); err != nil {
		t.Fatal(err)
	}
	if err := second.Dispatch(t.Context(), one, "wrong bus"); !errors.Is(err, fault.Invalid) {
		t.Fatal("bus schema leaked across registration boundaries", err)
	}
	if one == two || firstCalls.Load() != 1 || secondCalls.Load() != 1 {
		t.Fatal("event buses shared runtime state")
	}
}

func TestApplicationKeepsDependenciesAliveForAnUncooperativeEventHandler(t *testing.T) {
	key := foundation.NewKey[*events.Bus]("events")
	topic := events.Define[int]("test.shutdown", 1)
	var dependencyClosed atomic.Bool
	entered, unblock := make(chan struct{}), make(chan struct{})
	var released sync.Once
	dependency := foundation.Module{Name: "dependency", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		return r.OnShutdown("dependency", func(context.Context) error { dependencyClosed.Store(true); return nil })
	}}
	module := events.Module("events", key, events.DefaultConfig())
	module.Requires = []foundation.ProviderID{"dependency"}
	domain := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{"events"}, OnRegister: func(r *foundation.Registrar) error {
		return events.RegisterListener(r, key, topic, "slow", func(foundation.Resolver) (events.Handler[int], error) {
			return func(ctx context.Context, _ int) error {
				close(entered)
				<-unblock
				if dependencyClosed.Load() {
					return errors.New("event dependency closed before its handler exited")
				}
				return ctx.Err()
			}, nil
		})
	}}
	app := testkit.Start(t, foundry.New().Register(domain, module, dependency))
	t.Cleanup(func() { released.Do(func() { close(unblock) }) })
	bus, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- topic.Dispatch(t.Context(), bus, 7) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not start")
	}
	wait, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := app.Shutdown(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown abandoned the handler", err)
	}
	if dependencyClosed.Load() {
		t.Fatal("dependency closed while event remained active")
	}
	released.Do(func() { close(unblock) })
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("handler did not receive application cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not release")
	}
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !dependencyClosed.Load() {
		t.Fatal("dependency cleanup never completed")
	}
}

func TestBusZeroAndCanceledStartupAreOrdinaryErrors(t *testing.T) {
	var zero events.Bus
	if err := zero.Close(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero bus shutdown accepted", err)
	}
	if err := zero.Start(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero bus startup accepted", err)
	}
	bus, err := events.Prepare(events.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := bus.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled startup succeeded", err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal("canceled attempt prevented a valid start", err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal("running startup was not idempotent", err)
	}
	if err := bus.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal("closed bus restarted", err)
	}
}
