// Package events records explicitly selected production event listeners. It
// never replaces global dispatch or changes transaction/after-commit timing.
package events

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxEvents = 10000
const MaxRecordedBytes = 16 << 20

// Recorder stores immutable typed payload snapshots in arrival order. The
// returned listener can coexist with real listeners in the ordinary registry.
// A dispatch failure does not undo earlier listener observations; use the
// production Topic.AfterCommit boundary when recording requires committed work.
type Recorder[E any] struct {
	mu              sync.Mutex
	capacity, bytes int
	payloads        []value.JSON[E]
}

func New[E any](capacity int) (*Recorder[E], error) {
	if capacity < 1 || capacity > MaxEvents {
		return nil, fault.New(fault.Invalid, "invalid event recorder capacity")
	}
	return &Recorder[E]{capacity: capacity}, nil
}
func (r *Recorder[E]) Listener(id events.ListenerID) events.Listener[E] {
	return events.Listen(id, r.record)
}
func (r *Recorder[E]) record(ctx context.Context, input E) error {
	if r == nil || r.capacity == 0 || ctx == nil {
		return fault.New(fault.Invalid, "uninitialized event recorder")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var captured value.JSON[E]
	var size int
	err := callback.Isolated("record event snapshot", func() error {
		var err error
		captured, err = value.NewJSON(input)
		if err != nil {
			return err
		}
		text, err := captured.Text()
		size = len(text)
		return err
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(r.payloads) >= r.capacity || size > MaxRecordedBytes-r.bytes {
		return fault.New(fault.Invalid, "event recorder capacity reached")
	}
	r.payloads = append(r.payloads, captured)
	r.bytes += size
	return nil
}
func (r *Recorder[E]) Count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.payloads)
}

// Payloads returns independent decoded values. It never invokes codecs under
// the recorder lock; a failed decode returns no partial collection.
func (r *Recorder[E]) Payloads(ctx context.Context) ([]E, error) {
	if r == nil || r.capacity == 0 || ctx == nil {
		return nil, fault.New(fault.Invalid, "uninitialized event recorder")
	}
	r.mu.Lock()
	snapshots := slices.Clone(r.payloads)
	r.mu.Unlock()
	result := make([]E, len(snapshots))
	err := callback.Isolated("decode recorded events", func() error {
		for i, snapshot := range snapshots {
			if err := ctx.Err(); err != nil {
				return err
			}
			item, err := snapshot.Decode()
			if err != nil {
				return err
			}
			result[i] = item
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (*Recorder[E]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("event recorder")) }
func AssertCount[E any](t testing.TB, recorder *Recorder[E], want int) {
	t.Helper()
	if got := recorder.Count(); got != want {
		t.Errorf("recorded event count: got %d, want %d", got, want)
	}
}

// Start owns a normal production bus with exactly the supplied declarations.
// To test a complete application use testkit.Start and register the recorder's
// listener through that application's production events module instead.
func Start(t testing.TB, declarations ...events.Declaration) *events.Bus {
	t.Helper()
	bus, err := events.Prepare(events.DefaultConfig(), declarations...)
	if err != nil {
		t.Fatalf("prepare test event bus: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := bus.Close(ctx); err != nil {
			t.Errorf("close test event bus: %v", err)
		}
	})
	if err := bus.Start(t.Context()); err != nil {
		t.Fatalf("start test event bus: %v", err)
	}
	return bus
}
