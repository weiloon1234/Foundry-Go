package schedule_test

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type cyclicScheduleError struct{ visits atomic.Int32 }

func (*cyclicScheduleError) Error() string { panic("private schedule error must not be formatted") }
func (e *cyclicScheduleError) Unwrap() error {
	// Let an old implementation terminate with a finite failing visit count.
	if e.visits.Add(1) > 512 {
		return nil
	}
	return e
}

func TestCyclicScheduleErrorsReleaseCapacityAndOverlap(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		for _, stage := range []string{"handler", "before", "failed"} {
			name := stage
			if overlap {
				name += "/overlap"
			}
			t.Run(name, func(t *testing.T) {
				failure := &cyclicScheduleError{}
				options := schedule.DefaultOptions()
				options.WithoutOverlap = overlap
				handler := func(context.Context, schedule.Invocation) error { return failure }
				want := schedule.HandlerFailed
				if stage == "before" {
					options.Before = handler
					handler = func(context.Context, schedule.Invocation) error {
						t.Error("failed before hook reached handler")
						return nil
					}
					want = schedule.HookFailed
				} else if stage == "failed" {
					handler = func(context.Context, schedule.Invocation) error { return errors.New("ordinary failure") }
					options.Failed = func(context.Context, schedule.Invocation, error) error { return failure }
				}
				f := newFixture(t,
					every(t, "cyclic", handler, options),
					every(t, "healthy", func(context.Context, schedule.Invocation) error { return nil }),
				)
				f.start(t)
				for round := 1; round <= 2; round++ {
					f.advance(time.Minute)
					waitFor(t, func() bool {
						snapshot := f.scheduler.Snapshot()
						return snapshot.Active == 0 && len(snapshot.History) == round*2
					})
				}
				if failure.visits.Load() > 256 {
					t.Fatal("cyclic error exceeded two bounded inspections", failure.visits.Load())
				}
				for _, record := range f.scheduler.Snapshot().History {
					if record.Invocation.Schedule == "cyclic" {
						if record.State != schedule.Failed || record.Reason != want {
							t.Fatal("cyclic failure lost its execution stage")
						}
					} else if record.State != schedule.Succeeded {
						t.Fatal("unrelated schedule did not finish")
					}
				}
				if err := f.scheduler.Stop(t.Context()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

type failingOverlapRelease struct {
	lease.Backend
	key     lease.Key
	failure error
	enabled atomic.Bool
}

func (b *failingOverlapRelease) LeaseRelease(ctx context.Context, key lease.Key, owner lease.Owner) (bool, error) {
	released, err := b.Backend.LeaseRelease(ctx, key, owner)
	if err == nil && key == b.key && b.enabled.Load() {
		return released, b.failure
	}
	return released, err
}

func TestOverlapCleanupErrorInspectionReleasesSchedulerCapacity(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := memory.New(128)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { raw.Close() })
			namespace := keyspace.Namespace{Application: "schedule", Environment: "cleanup-inspection"}
			key, err := lease.NewKey(namespace, "foundry.schedule.overlap", "default:cleanup")
			if err != nil {
				t.Fatal(err)
			}
			cycle := new(cyclicScheduleError)
			var failure error = cycle
			if mode == "panic" {
				failure = inspectionError{func() { panic("private cleanup") }}
			}
			if mode == "goexit" {
				failure = inspectionError{runtime.Goexit}
			}
			backend := &failingOverlapRelease{Backend: raw, key: key, failure: failure}
			backend.enabled.Store(true)
			manager, err := lease.NewManager(backend, lease.DefaultConfig(namespace))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				// The injected lost release acknowledgement remains a cleanup diagnostic.
				if err := manager.Close(ctx); err == nil {
					t.Error("lease manager lost the cleanup failure")
				}
				select {
				case <-manager.Done():
				default:
					t.Error("lease manager did not drain")
				}
			})
			options := schedule.DefaultOptions()
			options.WithoutOverlap = true
			registry, err := schedule.NewRegistry(every(t, "cleanup", func(context.Context, schedule.Invocation) error { return nil }, options))
			if err != nil {
				t.Fatal(err)
			}
			clock := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
			wake := make(chan struct{}, 1)
			config := schedule.DefaultConfig("default")
			config.Clock, config.Wake, config.PollInterval = clock, wake, time.Millisecond
			config.Concurrency = 1
			scheduler, err := schedule.New(manager, registry, config)
			if err != nil {
				t.Fatal(err)
			}
			f := fixture{scheduler, clock, wake}
			f.start(t)
			f.advance(time.Minute)
			waitFor(t, func() bool { s := scheduler.Snapshot(); return s.Active == 0 && len(s.History) == 1 })
			first := scheduler.Snapshot().History[0]
			if first.State != schedule.Failed || first.Reason != schedule.CoordinationFailed {
				t.Fatal("cleanup failure lost its terminal coordination result")
			}
			if mode == "cycle" && (cycle.visits.Load() == 0 || cycle.visits.Load() > 256) {
				t.Fatal("cleanup classification exceeded its bound")
			}
			backend.enabled.Store(false)
			f.advance(time.Minute)
			waitFor(t, func() bool { s := scheduler.Snapshot(); return s.Active == 0 && len(s.History) == 2 })
			if scheduler.Snapshot().History[1].State != schedule.Succeeded {
				t.Fatal("later schedule could not reuse released capacity and lease")
			}
			if err := scheduler.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
