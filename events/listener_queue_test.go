package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/eventseam"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/inline"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Registered struct {
	User string `json:"user"`
}

// orderSubscriber groups two listeners of one event in a single type.
type orderSubscriber struct {
	topic events.Topic[Registered]
	log   func(string)
}

func (s orderSubscriber) Declarations() ([]events.Declaration, error) {
	declaration, err := s.topic.Declare(
		events.Listen("audit", func(_ context.Context, r Registered) error { s.log("audit:" + r.User); return nil }),
		events.Listen("welcome", func(_ context.Context, r Registered) error { s.log("welcome:" + r.User); return nil }).Queued(),
	)
	return []events.Declaration{declaration}, err
}

func TestQueuedListenersMixWithSyncListenersThroughTheJobQueue(t *testing.T) {
	topic := events.Define[Registered]("users.registered", 1)
	var mu sync.Mutex
	var order []string
	log := func(entry string) { mu.Lock(); defer mu.Unlock(); order = append(order, entry) }
	declarations, err := events.Subscribe(orderSubscriber{topic: topic, log: log})
	if err != nil {
		t.Fatal(err)
	}
	extra := declaration(t, topic, events.Listen("metrics", func(_ context.Context, r Registered) error { log("metrics:" + r.User); return nil }))
	bus := startedBus(t, events.DefaultConfig(), append(declarations, extra)...)
	// Without a bound queue, a queued listener cannot be delivered.
	if err := topic.Dispatch(t.Context(), bus, Registered{User: "early"}); !errors.Is(err, fault.Invalid) {
		t.Fatal("queued listener ran without a queue", err)
	}
	order = nil
	queue, err := events.NewListenerQueue(bus, jobs.DefaultPolicy("events"))
	if err != nil {
		t.Fatal(err)
	}
	jobDeclaration, err := queue.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(jobDeclaration)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := inline.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: "events", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Bind(dispatcher); err != nil {
		t.Fatal(err)
	}
	if err := topic.Dispatch(t.Context(), bus, Registered{User: "ada"}); err != nil {
		t.Fatal(err)
	}
	// The sync driver runs the queued listener's job during its enqueue.
	want := []string{"audit:ada", "welcome:ada", "metrics:ada"}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != len(want) {
		t.Fatal("listener order", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatal("listener order", order)
		}
	}
	if _, err := events.NewListenerQueue(bus, jobs.DefaultPolicy("events")); err == nil {
		t.Fatal("second listener queue accepted for one bus")
	}
}

func TestInterceptionRequiresTheFrameworkSeamToken(t *testing.T) {
	bus := startedBus(t, events.DefaultConfig())
	if _, err := bus.Intercept(eventseam.Token{}, func(context.Context, events.Name, events.Version, string) bool { return true }); !errors.Is(err, fault.Invalid) {
		t.Fatal("a zero token installed an interceptor", err)
	}
}
