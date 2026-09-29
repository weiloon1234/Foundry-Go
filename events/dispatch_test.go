package events_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func declaration[E any](t *testing.T, topic events.Topic[E], listeners ...events.Listener[E]) events.Declaration {
	t.Helper()
	result, err := topic.Declare(listeners...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func startedBus(t *testing.T, config events.Config, declarations ...events.Declaration) *events.Bus {
	t.Helper()
	bus, err := events.Prepare(config, declarations...)
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := bus.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return bus
}

type notice struct {
	ID     int               `json:"id"`
	Values map[string]string `json:"values,omitempty"`
	Tags   []string          `json:"tags,omitempty"`
}

func TestDispatchOrdersListenersAndIsolatesPayloadCopies(t *testing.T) {
	topic := events.Define[notice]("members.changed", 1)
	var order []string
	var captured attribution.Origin
	first := declaration(t, topic, events.Listen("first", func(ctx context.Context, input notice) error {
		order = append(order, "first")
		input.Values["key"] = "changed"
		input.Tags[0] = "changed"
		captured = attribution.FromContext(ctx)
		return nil
	}))
	second := declaration(t, topic, events.Listen("second", func(_ context.Context, input notice) error {
		order = append(order, "second")
		if input.Values["key"] != "original" || input.Tags[0] != "original" {
			return errors.New("listener received another listener's mutation")
		}
		return nil
	}))
	bus := startedBus(t, events.DefaultConfig(), first, second)
	origin, err := (attribution.Origin{}).WithSystem("test.runner")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	input := notice{ID: 7, Values: map[string]string{"key": "original"}, Tags: []string{"original"}}
	if err := topic.Dispatch(ctx, bus, input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) || captured != origin || input.Values["key"] != "original" || input.Tags[0] != "original" {
		t.Fatal("dispatch lost order, provenance or caller ownership")
	}
}

func TestDispatchContainsFailuresAndReleasesAdmission(t *testing.T) {
	for _, kind := range []string{"error", "panic", "exit"} {
		t.Run(kind, func(t *testing.T) {
			topic := events.Define[int]("test.failure", 1)
			later := false
			sentinel := errors.New("listener rejected")
			config := events.DefaultConfig()
			config.MaxInFlight = 1
			bus := startedBus(t, config, declaration(t, topic,
				events.Listen("first", func(_ context.Context, input int) error {
					if input == 0 {
						return nil
					}
					switch kind {
					case "panic":
						panic("private payload")
					case "exit":
						runtime.Goexit()
					}
					return sentinel
				}), events.Listen("later", func(context.Context, int) error { later = true; return nil })))
			err := topic.Dispatch(t.Context(), bus, 1)
			expected := error(fault.Panicked)
			if kind == "error" {
				expected = sentinel
			}
			if !errors.Is(err, expected) || strings.Contains(err.Error(), "private") || later {
				t.Fatal("listener failure escaped, leaked or ran later listener", err)
			}
			if err := topic.Dispatch(t.Context(), bus, 0); err != nil || !later {
				t.Fatal("failed dispatch leaked admission", err)
			}
		})
	}
}

func TestDispatchCapacityAndShutdownRetainActualCallbackOwnership(t *testing.T) {
	topic := events.Define[int]("test.blocking", 1)
	entered, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	config := events.DefaultConfig()
	config.MaxInFlight = 1
	bus := startedBus(t, config, declaration(t, topic, events.Listen("blocked", func(ctx context.Context, _ int) error {
		close(entered)
		<-unblock
		return ctx.Err()
	})))
	t.Cleanup(func() { once.Do(func() { close(unblock) }) })
	completed := make(chan error, 1)
	go func() { completed <- topic.Dispatch(t.Context(), bus, 1) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not start")
	}
	// A burst beyond MaxInFlight waits briefly, then fails as retryable overload.
	short, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	err := topic.Dispatch(short, bus, 2)
	stop()
	if !errors.Is(err, fault.Overloaded) {
		t.Fatal("over-capacity dispatch was admitted", err)
	}
	wait, cancel := context.WithCancel(t.Context())
	cancel()
	if err := bus.Close(wait); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown did not respect wait cancellation", err)
	}
	select {
	case <-bus.Done():
		t.Fatal("shutdown claimed a blocked callback exited")
	default:
	}
	if err := topic.Dispatch(t.Context(), bus, 3); !errors.Is(err, fault.Closed) {
		t.Fatal("closing bus admitted work", err)
	}
	once.Do(func() { close(unblock) })
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("lifetime cancellation was lost", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch did not release")
	}
	if err := bus.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchBeyondCapacityQueuesUntilASlotFrees(t *testing.T) {
	topic := events.Define[int]("test.queued", 1)
	entered, unblock := make(chan struct{}, 2), make(chan struct{})
	config := events.DefaultConfig()
	config.MaxInFlight = 1
	bus := startedBus(t, config, declaration(t, topic, events.Listen("queued", func(ctx context.Context, input int) error {
		entered <- struct{}{}
		if input == 1 {
			<-unblock
		}
		return nil
	})))
	first := make(chan error, 1)
	go func() { first <- topic.Dispatch(t.Context(), bus, 1) }()
	<-entered
	second := make(chan error, 1)
	go func() { second <- topic.Dispatch(t.Context(), bus, 2) }()
	select {
	case <-entered:
		t.Fatal("dispatch exceeded MaxInFlight")
	case <-time.After(20 * time.Millisecond):
	}
	close(unblock)
	for _, result := range []chan error{first, second} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("queued dispatch was not admitted")
		}
	}
	if err := bus.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchDepthAndRetainedContextsTrackOnlyActiveWork(t *testing.T) {
	topic := events.Define[int]("test.recursion", 1)
	config := events.DefaultConfig()
	config.MaxDepth = 2
	var bus *events.Bus
	var saved context.Context
	handler := events.Listen("nested", func(ctx context.Context, input int) error {
		saved = ctx
		if input > 0 {
			return topic.Dispatch(ctx, bus, input-1)
		}
		if err := bus.Close(ctx); !errors.Is(err, fault.Cycle) {
			return errors.New("handler shutdown was not rejected")
		}
		return nil
	})
	bus = startedBus(t, config, declaration(t, topic, handler))
	if err := topic.Dispatch(t.Context(), bus, 3); !errors.Is(err, fault.Cycle) {
		t.Fatal("unbounded dispatch chain accepted", err)
	}
	retained := context.WithoutCancel(saved)
	if err := topic.Dispatch(retained, bus, 0); err != nil {
		t.Fatal("completed ancestors caused false recursion or shutdown", err)
	}
}

func TestRegistryRequiresMatchingTypesNamesVersionsAndUniqueListeners(t *testing.T) {
	topic := events.Define[int]("test.payload", 1)
	declared := declaration(t, topic)
	bus := startedBus(t, events.DefaultConfig(), declared)
	if err := topic.Dispatch(t.Context(), bus, 7); err != nil {
		t.Fatal("declared no-listener event failed", err)
	}
	if err := events.Define[string]("test.payload", 1).Dispatch(t.Context(), bus, "wrong"); !errors.Is(err, fault.Invalid) {
		t.Fatal("same name hid a different payload type", err)
	}
	if err := events.Define[int]("test.payload", 2).Dispatch(t.Context(), bus, 7); !errors.Is(err, fault.Missing) {
		t.Fatal("undeclared schema version accepted", err)
	}
	conflict := declaration(t, events.Define[string]("test.payload", 1))
	if _, err := events.Prepare(events.DefaultConfig(), declared, conflict); !errors.Is(err, fault.Duplicate) {
		t.Fatal("conflicting schema accepted", err)
	}
	version2 := declaration(t, events.Define[string]("test.payload", 2))
	prepared, err := events.Prepare(events.DefaultConfig(), declared, version2)
	if err != nil {
		t.Fatal(err)
	}
	if err := topic.Dispatch(t.Context(), prepared, 7); !errors.Is(err, fault.Closed) {
		t.Fatal("prepared bus ran listeners", err)
	}
	if err := prepared.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	listener := events.Listen("once", func(context.Context, int) error { return nil })
	if _, err := topic.Declare(listener, listener); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate declaration listener accepted", err)
	}
	one := declaration(t, topic, listener)
	if _, err := events.Prepare(events.DefaultConfig(), one, one); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate contributed listener accepted", err)
	}
	for _, invalid := range []events.Topic[int]{events.Define[int]("invalid name", 1), events.Define[int]("test.payload", 0), {}} {
		if _, err := invalid.Declare(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid topic accepted", err)
		}
	}
	if _, err := events.Define[any]("test.dynamic", 1).Declare(); !errors.Is(err, fault.Invalid) {
		t.Fatal("dynamic root payload type accepted", err)
	}
}
