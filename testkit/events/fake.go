package events

import (
	"context"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/internal/eventseam"
	"github.com/weiloon1234/Foundry-Go/value"
)

type dispatched struct {
	name    events.Name
	version events.Version
	payload string
}

// Fake intercepts every dispatch on one production bus for the rest of the
// test: listeners (sync and queued) do not run, and payload snapshots are
// recorded for typed assertions. Dispatch still validates and captures the
// payload exactly as in production. Cleanup restores the bus.
type Fake struct {
	mu       sync.Mutex
	recorded []dispatched
}

// NewFake installs the fake on bus; at most MaxEvents dispatches are recorded.
func NewFake(t testing.TB, bus *events.Bus) *Fake {
	t.Helper()
	fake := &Fake{}
	restore, err := bus.Intercept(eventseam.Grant(), func(_ context.Context, name events.Name, version events.Version, payload string) bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if len(fake.recorded) < MaxEvents {
			fake.recorded = append(fake.recorded, dispatched{name: name, version: version, payload: payload})
		}
		return true
	})
	if err != nil {
		t.Fatalf("install event fake: %v", err)
	}
	t.Cleanup(func() {
		restore()
		fake.mu.Lock()
		defer fake.mu.Unlock()
		clear(fake.recorded)
		fake.recorded = nil
	})
	return fake
}

// Dispatched decodes every recorded payload of topic in dispatch order.
func Dispatched[E any](t testing.TB, fake *Fake, topic events.Topic[E]) []E {
	t.Helper()
	fake.mu.Lock()
	var texts []string
	for _, item := range fake.recorded {
		if item.name == topic.Name() && item.version == topic.Version() {
			texts = append(texts, item.payload)
		}
	}
	fake.mu.Unlock()
	result := make([]E, 0, len(texts))
	for _, text := range texts {
		snapshot, err := value.ParseJSON[E](text)
		if err != nil {
			t.Fatal("decode faked event payload failed")
		}
		payload, err := snapshot.Decode()
		if err != nil {
			t.Fatal("decode faked event payload failed")
		}
		result = append(result, payload)
	}
	return result
}

func countDispatched[E any](t testing.TB, fake *Fake, topic events.Topic[E], match func(E) bool) int {
	t.Helper()
	count := 0
	for _, payload := range Dispatched(t, fake, topic) {
		if match == nil || match(payload) {
			count++
		}
	}
	return count
}

// AssertDispatched requires at least one dispatch of topic whose payload
// satisfies match (nil matches any). Failure text never includes payloads.
func AssertDispatched[E any](t testing.TB, fake *Fake, topic events.Topic[E], match func(E) bool) {
	t.Helper()
	if countDispatched(t, fake, topic, match) == 0 {
		t.Errorf("no matching %s v%d event was dispatched", topic.Name(), topic.Version())
	}
}

// AssertNotDispatched requires that no dispatch of topic satisfies match.
func AssertNotDispatched[E any](t testing.TB, fake *Fake, topic events.Topic[E], match func(E) bool) {
	t.Helper()
	if got := countDispatched(t, fake, topic, match); got != 0 {
		t.Errorf("%d matching %s v%d event(s) were dispatched", got, topic.Name(), topic.Version())
	}
}

// AssertDispatchedCount requires exactly want dispatches of topic satisfying
// match.
func AssertDispatchedCount[E any](t testing.TB, fake *Fake, topic events.Topic[E], match func(E) bool, want int) {
	t.Helper()
	if got := countDispatched(t, fake, topic, match); got != want {
		t.Errorf("dispatched %s v%d event count: got %d, want %d", topic.Name(), topic.Version(), got, want)
	}
}
