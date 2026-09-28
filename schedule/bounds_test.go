package schedule_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

func TestConcurrencyAndHistoryAreBoundedUntilActualExit(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	var declarations []schedule.Declaration
	for _, id := range []schedule.ID{"first", "second", "third"} {
		declarations = append(declarations, every(t, id, func(context.Context, schedule.Invocation) error { calls.Add(1); <-release; return nil }))
	}
	f := configuredFixture(t, func(c *schedule.Config) { c.Concurrency = 1; c.MaxHistory = 2 }, declarations...)
	f.start(t)
	f.advance(time.Minute)
	waitFor(t, func() bool { return calls.Load() == 1 && len(f.scheduler.Snapshot().History) == 2 })
	snapshot := f.scheduler.Snapshot()
	if snapshot.Active != 1 {
		t.Fatal("running callback does not own its slot")
	}
	for _, record := range snapshot.History {
		if record.Reason != schedule.CapacityReached {
			t.Fatal("capacity did not skip excess work")
		}
	}
	release <- struct{}{}
	waitFor(t, func() bool { return f.scheduler.Snapshot().Active == 0 })
	snapshot = f.scheduler.Snapshot()
	if len(snapshot.History) != 2 || snapshot.History[1].State != schedule.Succeeded || calls.Load() != 1 {
		t.Fatal("terminal result was lost after admission history eviction")
	}
}

func TestSchedulerDoesNotRetainCallerContextValues(t *testing.T) {
	type privateKey struct{}
	checked := make(chan bool, 1)
	f := newFixture(t, every(t, "context", func(ctx context.Context, _ schedule.Invocation) error {
		checked <- ctx.Value(privateKey{}) == nil
		return nil
	}))
	f.startContext(t, context.WithValue(t.Context(), privateKey{}, "request data"))
	f.advance(time.Minute)
	select {
	case ok := <-checked:
		if !ok {
			t.Fatal("request values leaked into schedule")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not run")
	}
}

func TestClosedWakeTerminatesOwnedRuntime(t *testing.T) {
	f := newFixture(t, every(t, "wake", func(context.Context, schedule.Invocation) error { return nil }))
	done := make(chan error, 1)
	go func() { done <- f.scheduler.Run(t.Context()) }()
	waitFor(t, func() bool { return f.scheduler.Snapshot().Leader })
	close(f.wake)
	select {
	case err := <-done:
		if !errors.Is(err, fault.Closed) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closed wake channel caused a busy loop")
	}
	select {
	case <-f.scheduler.Done():
	default:
		t.Fatal("runtime returned before owned drain")
	}
}
