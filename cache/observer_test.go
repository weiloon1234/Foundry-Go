package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// failingPublication stores nothing: every tagged write fails after the loader.
type failingPublication struct{ *memory.Backend }

func (failingPublication) PutTagged(context.Context, cache.TaggedKey, []byte, cache.TTL) error {
	return fault.New(fault.Overloaded, "cache write rejected")
}

type recordedEvents struct {
	mu     sync.Mutex
	events []cache.Event
}

func (r *recordedEvents) observe(_ context.Context, event cache.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}
func (r *recordedEvents) take() []cache.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := r.events
	r.events = nil
	return events
}

func TestObserverReportsTypedCallsWithoutKeysOrValues(t *testing.T) {
	_, backend, _ := store(t, nil)
	var recorded recordedEvents
	s, err := cache.NewStore(backend, cache.DefaultConfig(namespace), cache.WithObserver(recorded.observe))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "a", profile{Name: "a"}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Get(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetMany(t.Context(), "a", "b", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Remember(t.Context(), "r", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "r"}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Remember(t.Context(), "r", cache.Forever(), func(context.Context) (profile, error) { return profile{}, errors.New("not called") }); err != nil {
		t.Fatal(err)
	}
	if err := s.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	events := recorded.take()
	want := []cache.Event{
		{Family: "profiles", Operation: cache.OperationPut},
		{Family: "profiles", Operation: cache.OperationGet, Hits: 1},
		{Family: "profiles", Operation: cache.OperationGetMany, Hits: 2, Misses: 1},
		{Family: "profiles", Operation: cache.OperationRemember, Misses: 1, Loaded: true},
		{Family: "profiles", Operation: cache.OperationRemember, Hits: 1},
		{Operation: cache.OperationInvalidate},
	}
	if len(events) != len(want) {
		t.Fatal("unexpected events", events)
	}
	for i, event := range events {
		if event.Duration < 0 {
			t.Fatal("negative duration", event)
		}
		event.Duration = 0
		if event != want[i] {
			t.Fatal("event", i, event, want[i])
		}
	}
	if cache.OperationGetMany.String() != "get_many" || cache.Operation(0).String() != "unknown" {
		t.Fatal("operation names changed")
	}
	// Failures carry their framework classification; an unstored Remember value
	// is reported while the caller still receives it.
	if _, err := c.GetMany(t.Context(), make([]userKey, 65)...); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if events := recorded.take(); len(events) != 1 || events[0].Code != fault.Invalid || events[0].Operation != cache.OperationGetMany {
		t.Fatal("failure event", events)
	}
	failing, err := cache.NewStore(failingPublication{backend}, cache.DefaultConfig(namespace), cache.WithObserver(recorded.observe))
	if err != nil {
		t.Fatal(err)
	}
	unstored, err := profiles.Bind(failing)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := unstored.Remember(t.Context(), "u", cache.Forever(), func(context.Context) (profile, error) { return profile{Name: "u"}, nil }); err != nil || value.Name != "u" {
		t.Fatal(value, err)
	}
	if events := recorded.take(); len(events) != 1 || !events[0].Unpublished || !events[0].Loaded || events[0].Code != "" {
		t.Fatal("unpublished event", events)
	}
	// A panicking observer never changes the result.
	panicking, err := cache.NewStore(backend, cache.DefaultConfig(namespace), cache.WithObserver(func(context.Context, cache.Event) { panic("observer") }))
	if err != nil {
		t.Fatal(err)
	}
	contained, err := profiles.Bind(panicking)
	if err != nil {
		t.Fatal(err)
	}
	if err := contained.Put(t.Context(), "p", profile{Name: "p"}, cache.Forever()); err != nil {
		t.Fatal("observer panic changed the result", err)
	}
	if _, err := cache.NewStore(backend, cache.DefaultConfig(namespace), cache.WithObserver(nil)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := cache.NewStore(backend, cache.DefaultConfig(namespace), cache.WithObserver(recorded.observe), cache.WithObserver(recorded.observe)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
