// Package scheduletest exercises scheduler ownership against shared lease backends.
package scheduletest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type authority struct {
	lease.Backend
	mu         sync.Mutex
	first      lease.Key
	seen       map[lease.Key]bool
	track      func(string)
	denyLeader atomic.Bool
	attempts   atomic.Uint32
}

func (b *authority) leader(key lease.Key) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.first == (lease.Key{}) {
		b.first = key
	}
	if !b.seen[key] {
		b.seen[key] = true
		if b.track != nil {
			b.track(key.String())
		}
	}
	return b.first == key
}
func (b *authority) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	if b.leader(key) {
		if b.denyLeader.Load() {
			return false, nil
		}
		switch b.attempts.Add(1) {
		case 1:
			// Simulate a dead process's outstanding lease. The scheduler must wait
			// for real backend expiry before becoming leader.
			foreign, err := lease.NewOwner()
			if err != nil {
				return false, err
			}
			_, err = b.Backend.LeaseAcquire(ctx, key, foreign, 50*time.Millisecond)
			return false, err
		case 2:
			return false, fault.New(fault.Internal, "injected coordination outage")
		}
	}
	return b.Backend.LeaseAcquire(ctx, key, owner, ttl)
}
func (b *authority) LeaseRenew(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	if b.leader(key) && b.denyLeader.Load() {
		return false, nil
	}
	return b.Backend.LeaseRenew(ctx, key, owner, ttl)
}
func wait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("coordinated scheduler condition not reached")
}

// Run verifies a single leader, leadership loss, takeover and overlap renewal
// while an old epoch's canceled handler deliberately refuses to exit. The same
// contract executes against real Redis and the explicit local backend.
func Run(t *testing.T, backend lease.Backend, namespace keyspace.Namespace, track func(string)) {
	t.Helper()
	const ttl = 300 * time.Millisecond
	primary := &authority{Backend: backend, seen: make(map[lease.Key]bool), track: track}
	clock := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	d, err := schedule.Every("shared.task", time.Minute, func(ctx context.Context, _ schedule.Invocation) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	options := schedule.DefaultOptions()
	options.WithoutOverlap = true
	options.OverlapTTL = ttl
	d, err = d.With(options)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := schedule.NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	makeScheduler := func(authority lease.Backend) (*schedule.Scheduler, chan struct{}) {
		config := lease.DefaultConfig(namespace)
		config.OperationTimeout = 100 * time.Millisecond
		manager, err := lease.NewManager(authority, config)
		if err != nil {
			t.Fatal(err)
		}
		wake := make(chan struct{}, 1)
		runtime := schedule.DefaultConfig("contract")
		runtime.Clock = clock
		runtime.Wake = wake
		runtime.LeadershipTTL = ttl
		runtime.PollInterval = 5 * time.Millisecond
		s, err := schedule.New(manager, registry, runtime)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- s.Run(t.Context()) }()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.Stop(ctx); err != nil {
				t.Error(err)
				return
			}
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-ctx.Done():
				t.Error("scheduler failed to drain")
				return
			}
			if err := manager.Close(ctx); err != nil {
				t.Error(err)
			}
		})
		return s, wake
	}
	first, wakeFirst := makeScheduler(primary)
	wait(t, func() bool { return first.Snapshot().Leader })
	if first.Snapshot().CoordinationFailures == 0 {
		t.Fatal("coordination outage was not recorded")
	}
	second, wakeSecond := makeScheduler(backend)
	advance := func() {
		clock.Advance(time.Minute)
		for _, wake := range []chan struct{}{wakeFirst, wakeSecond} {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
	advance()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not invoke task")
	}
	if second.Snapshot().Leader || calls.Load() != 1 {
		t.Fatal("multiple schedulers held leadership")
	}
	primary.denyLeader.Store(true)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("leadership loss did not cancel work")
	}
	wait(t, func() bool { return second.Snapshot().Leader })
	// Lease safety intentionally uses real time. Cross two original TTLs to
	// prove retained overlap ownership while application time remains controlled.
	time.Sleep(2 * ttl)
	advance()
	wait(t, func() bool { s := second.Snapshot(); return len(s.History) > 0 && s.Active == 0 })
	if second.Snapshot().History[0].Reason != schedule.OverlapBusy || calls.Load() != 1 || first.Snapshot().Active != 1 {
		t.Fatal("known live callback lost overlap protection after leadership loss")
	}
	release <- struct{}{}
	wait(t, func() bool { return first.Snapshot().Active == 0 })
	advance()
	wait(t, func() bool { return calls.Load() == 2 && second.Snapshot().Active == 0 })
	if first.Snapshot().History[0].Reason != schedule.LeadershipLost {
		t.Fatal("leadership loss not recorded")
	}
}
