package schedule_test

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type fixture struct {
	scheduler *schedule.Scheduler
	clock     *testkit.Clock
	wake      chan struct{}
}

func newFixture(t *testing.T, declarations ...schedule.Declaration) fixture {
	return configuredFixture(t, nil, declarations...)
}
func configuredFixture(t *testing.T, configure func(*schedule.Config), declarations ...schedule.Declaration) fixture {
	t.Helper()
	backend, err := memory.New(128)
	if err != nil {
		t.Fatal(err)
	}
	namespace := keyspace.Namespace{Application: "schedule", Environment: "test"}
	manager, err := lease.NewManager(backend, lease.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	clock := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	wake := make(chan struct{}, 1)
	config := schedule.DefaultConfig("default")
	config.Clock = clock
	config.Wake = wake
	config.PollInterval = time.Millisecond
	if configure != nil {
		configure(&config)
	}
	registry, err := schedule.NewRegistry(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := schedule.New(manager, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := scheduler.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
			return
		}
		backend.Close()
	})
	return fixture{scheduler, clock, wake}
}
func every(t *testing.T, id schedule.ID, handler schedule.Handler, options ...schedule.Options) schedule.Declaration {
	t.Helper()
	d, err := schedule.Every(id, time.Minute, handler)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 0 {
		d, err = d.With(options[0])
		if err != nil {
			t.Fatal(err)
		}
	}
	return d
}
func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("scheduler condition did not become true")
}
func (f fixture) start(t *testing.T) {
	f.startContext(t, t.Context())
}
func (f fixture) startContext(t *testing.T, ctx context.Context) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f.scheduler.Run(ctx) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := f.scheduler.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("scheduler did not return")
		}
	})
	waitFor(t, func() bool { return f.scheduler.Snapshot().Leader })
}
func (f fixture) advance(duration time.Duration) { f.clock.Advance(duration); f.wake <- struct{}{} }

func TestOwnedInvocationAttributionAndSavedContext(t *testing.T) {
	contexts := make(chan context.Context, 1)
	var f fixture
	f = newFixture(t, every(t, "reports.daily", func(ctx context.Context, invocation schedule.Invocation) error {
		current, ok := schedule.Current(ctx)
		if !ok || current.Occurrence != invocation.Occurrence || current.Occurrence.IsZero() {
			return errors.New("missing occurrence")
		}
		if attribution.FromContext(ctx).System() != "reports.daily" {
			return errors.New("missing schedule attribution")
		}
		if err := f.scheduler.Stop(ctx); !errors.Is(err, fault.Cycle) {
			return errors.New("self-wait was not rejected")
		}
		contexts <- ctx
		return nil
	}))
	f.start(t)
	f.advance(time.Minute)
	waitFor(t, func() bool {
		s := f.scheduler.Snapshot()
		return len(s.History) == 1 && s.History[0].State == schedule.Succeeded
	})
	if _, ok := schedule.Current(<-contexts); ok {
		t.Fatal("saved context retained execution")
	}
	snapshot := f.scheduler.Snapshot()
	snapshot.History[0].Reason = schedule.Panicked
	if f.scheduler.Snapshot().History[0].Reason != schedule.NoReason {
		t.Fatal("history was aliased")
	}
}

func TestDefaultSkipsRestartBacklogAndMissedTicks(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, every(t, "skip", func(context.Context, schedule.Invocation) error { calls.Add(1); return nil }))
	f.start(t)
	if calls.Load() != 0 {
		t.Fatal("startup replayed old occurrences")
	}
	f.advance(10 * time.Minute)
	waitFor(t, func() bool { return len(f.scheduler.Snapshot().History) != 0 })
	if calls.Load() != 0 || f.scheduler.Snapshot().History[0].Reason != schedule.Missed {
		t.Fatal("missed ticks replayed implicitly")
	}
	f.advance(time.Minute)
	waitFor(t, func() bool { return calls.Load() == 1 })
	f.advance(-30 * time.Second)
	waitFor(t, func() bool { return f.scheduler.Snapshot().Active == 0 })
	if calls.Load() != 1 {
		t.Fatal("backward clock repeated occurrence")
	}
}

func TestCatchUpHasWindowAndCountBounds(t *testing.T) {
	occurrences := make(chan schedule.Invocation, 10)
	options := schedule.DefaultOptions()
	options.CatchUp = schedule.CatchUp{Window: 10 * time.Minute, Max: 2}
	f := newFixture(t, every(t, "catch.up", func(_ context.Context, invocation schedule.Invocation) error { occurrences <- invocation; return nil }, options))
	f.start(t)
	waitFor(t, func() bool { s := f.scheduler.Snapshot(); return len(s.History) >= 3 && s.Active == 0 })
	if len(occurrences) != 2 {
		t.Fatal("catch-up count was not bounded")
	}
	// A backlog beyond Max runs the most recent occurrences, not the oldest.
	now := f.clock.Now()
	first, second := (<-occurrences).IntendedAt, (<-occurrences).IntendedAt
	if first.After(second) {
		first, second = second, first
	}
	if !first.Equal(now.Add(-time.Minute)) || !second.Equal(now) {
		t.Fatal("catch-up did not select the most recent occurrences", first, second)
	}
	limited := false
	for _, record := range f.scheduler.Snapshot().History {
		limited = limited || record.Reason == schedule.BacklogLimited && record.Invocation.IntendedAt.Equal(now.Add(-10*time.Minute))
	}
	if !limited {
		t.Fatal("discarded backlog not inspected")
	}
}

func TestPanicGoexitAndHookFailureDoNotStopOtherSchedules(t *testing.T) {
	var completed atomic.Int32
	badHook := schedule.DefaultOptions()
	badHook.Before = func(context.Context, schedule.Invocation) error { return errors.New("hook failure") }
	badHook.Failed = func(context.Context, schedule.Invocation, error) error { panic("private") }
	f := newFixture(t,
		every(t, "panic", func(context.Context, schedule.Invocation) error { panic("private") }),
		every(t, "goexit", func(context.Context, schedule.Invocation) error { runtime.Goexit(); return nil }),
		every(t, "hook", func(context.Context, schedule.Invocation) error {
			t.Error("failed before hook ran handler")
			return nil
		}, badHook),
		every(t, "healthy", func(context.Context, schedule.Invocation) error { completed.Add(1); return nil }),
	)
	f.start(t)
	f.advance(time.Minute)
	waitFor(t, func() bool { s := f.scheduler.Snapshot(); return len(s.History) == 4 && s.Active == 0 })
	if completed.Load() != 1 {
		t.Fatal("failed tasks blocked healthy task")
	}
	f.advance(time.Minute)
	waitFor(t, func() bool { return completed.Load() == 2 })
}

func TestEnvironmentFiltersAndUncooperativeShutdown(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{})
	timedOut := make(chan struct{})
	options := schedule.DefaultOptions()
	options.Timeout = 20 * time.Millisecond
	options.WithoutOverlap = true
	filtered := schedule.DefaultOptions()
	filtered.Environments = []string{"production"}
	f := newFixture(t,
		every(t, "long", func(ctx context.Context, _ schedule.Invocation) error {
			close(entered)
			<-ctx.Done()
			close(timedOut)
			<-release
			return nil
		}, options),
		every(t, "filtered", func(context.Context, schedule.Invocation) error { t.Error("environment filter ignored"); return nil }, filtered),
	)
	f.start(t)
	f.advance(time.Minute)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("task not entered")
	}
	select {
	case <-timedOut:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout not delivered")
	}
	if f.scheduler.Snapshot().Active != 1 {
		t.Fatal("timeout abandoned live callback")
	}
	f.advance(time.Minute)
	waitFor(t, func() bool { return len(f.scheduler.Snapshot().History) == 2 })
	if f.scheduler.Snapshot().History[1].Reason != schedule.OverlapBusy {
		t.Fatal("live callback overlapped")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	err := f.scheduler.Stop(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-f.scheduler.Done():
		t.Fatal("scheduler abandoned callback")
	default:
	}
	release <- struct{}{}
	waitFor(t, func() bool { return f.scheduler.Snapshot().Active == 0 })
	if f.scheduler.Snapshot().History[0].Reason != schedule.TimedOut {
		t.Fatal("timeout classification lost")
	}
}
