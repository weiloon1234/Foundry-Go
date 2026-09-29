package schedule_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

// cursorBackend adds persisted catch-up cursors to the memory lease backend,
// standing in for a shared coordination store across scheduler processes.
type cursorBackend struct {
	*memory.Backend
	mu      sync.Mutex
	cursors map[lease.Key]time.Time
}

func (b *cursorBackend) ScheduleCursor(_ context.Context, key lease.Key) (time.Time, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	at, ok := b.cursors[key]
	return at, ok, nil
}

func (b *cursorBackend) AdvanceScheduleCursor(_ context.Context, key lease.Key, at time.Time, _ time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if at.After(b.cursors[key]) {
		b.cursors[key] = at
	}
	return nil
}

// A restarted scheduler resumes catch-up after the last handled occurrence
// instead of replaying occurrences a previous process already completed.
func TestPersistedCursorPreventsCatchUpReplayAfterRestart(t *testing.T) {
	store, err := memory.New(128)
	if err != nil {
		t.Fatal(err)
	}
	backend := &cursorBackend{Backend: store, cursors: make(map[lease.Key]time.Time)}
	manager, err := lease.NewManager(backend, lease.DefaultConfig(keyspace.Namespace{Application: "schedule", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()); store.Close() })
	clock := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	var calls atomic.Int32
	options := schedule.DefaultOptions()
	options.CatchUp = schedule.CatchUp{Window: 5 * time.Minute, Max: 10}
	declaration := every(t, "cursor", func(context.Context, schedule.Invocation) error { calls.Add(1); return nil }, options)
	run := func() {
		wake := make(chan struct{}, 1)
		config := schedule.DefaultConfig("default")
		config.Clock, config.Wake, config.PollInterval = clock, wake, time.Millisecond
		registry, err := schedule.NewRegistry(declaration)
		if err != nil {
			t.Fatal(err)
		}
		scheduler, err := schedule.New(manager, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- scheduler.Run(t.Context()) }()
		waitFor(t, func() bool { return scheduler.Snapshot().Leader })
		// The second wake is buffered only after the first was consumed, so the
		// epoch's initial catch-up tick has completed.
		wake <- struct{}{}
		wake <- struct{}{}
		waitFor(t, func() bool { return scheduler.Snapshot().Active == 0 })
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := scheduler.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		<-done
	}
	run()
	if calls.Load() != 6 {
		t.Fatal("catch-up did not run the window's occurrences", calls.Load())
	}
	run()
	if calls.Load() != 6 {
		t.Fatal("restart replayed completed occurrences", calls.Load())
	}
}
