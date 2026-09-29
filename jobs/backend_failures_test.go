package jobs_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/value"
)

type workerInspectionError struct{ exit bool }

func (workerInspectionError) Error() string { return "backend failure" }
func (e workerInspectionError) Is(error) bool {
	if e.exit {
		runtime.Goexit()
	}
	panic("private backend state")
}

type failingWorkerBackend struct {
	jobs.Backend
	operation, mode string
	failure         error
	calls           *atomic.Int32
}

func (b failingWorkerBackend) fail() error {
	if b.calls != nil {
		b.calls.Add(1)
	}
	if b.failure != nil {
		return b.failure
	}
	switch b.mode {
	case "panic":
		panic("private backend state")
	case "goexit":
		runtime.Goexit()
	}
	return workerInspectionError{exit: b.mode == "inspect-goexit"}
}
func (b failingWorkerBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, duration time.Duration) (value.Optional[jobs.Reservation], error) {
	if b.operation == "reserve" {
		return value.Optional[jobs.Reservation]{}, b.fail()
	}
	return b.Backend.JobReserve(ctx, key, owner, duration)
}
func (b failingWorkerBackend) JobStart(ctx context.Context, key jobs.Key, owner jobs.Ownership) (uint32, error) {
	if b.operation == "start" {
		return 0, b.fail()
	}
	return b.Backend.JobStart(ctx, key, owner)
}
func (b failingWorkerBackend) JobRenew(ctx context.Context, key jobs.Key, owner jobs.Ownership, duration time.Duration) (jobs.LeaseStatus, error) {
	if b.operation == "renew" {
		return jobs.LeaseStatus{}, b.fail()
	}
	return b.Backend.JobRenew(ctx, key, owner, duration)
}
func (b failingWorkerBackend) JobFinish(ctx context.Context, key jobs.Key, owner jobs.Ownership, result jobs.Result) (bool, error) {
	if b.operation == "finish" {
		return false, b.fail()
	}
	return b.Backend.JobFinish(ctx, key, owner, result)
}

// Backend failures are survived: the worker logs them, backs off and keeps
// reserving. It never abandons an admitted handler, and bounded error
// inspection keeps each failure's traversal finite.
func TestWorkerSurvivesBackendFailuresAndRetainsShutdownOwnership(t *testing.T) {
	for _, operation := range []string{"reserve", "start", "renew", "finish"} {
		modes := []string{"panic", "goexit", "cycle"}
		if operation == "reserve" || operation == "start" {
			modes = append(modes, "inspect-panic", "inspect-goexit")
		}
		for _, mode := range modes {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				handler := func(ctx context.Context, _ payload) error {
					if operation == "renew" {
						<-ctx.Done()
						return ctx.Err()
					}
					return nil
				}
				f := newWorkerFixture(t, jobs.DefaultPolicy("default"), handler)
				f.enqueue(t)
				declaration, err := f.definition.Declare(handler)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := jobs.NewRegistry(declaration)
				if err != nil {
					t.Fatal(err)
				}
				config := jobs.DefaultWorkerConfig(f.key.Namespace(), "default")
				config.Concurrency, config.PollInterval, config.FailureBackoff = 1, time.Millisecond, 20*time.Millisecond
				config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout = 150*time.Millisecond, 15*time.Millisecond, 30*time.Millisecond
				cycle := &cyclicJobError{}
				calls := &atomic.Int32{}
				backend := failingWorkerBackend{Backend: f.backend, operation: operation, mode: mode, calls: calls}
				if mode == "cycle" {
					backend.failure = cycle
				}
				worker, err := jobs.NewWorker(backend, registry, config)
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- worker.Run(t.Context()) }()
				deadline := time.Now().Add(3 * time.Second)
				for calls.Load() < 2 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if calls.Load() < 2 {
					t.Fatal("worker stopped retrying after a backend failure")
				}
				select {
				case err := <-done:
					t.Fatal("backend failure stopped the worker", err)
				case <-worker.Done():
					t.Fatal("backend failure closed the worker")
				default:
				}
				stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := worker.Stop(stop); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatal("stopped worker reported a failure", err)
				}
				if visits := cycle.visits.Load(); visits > 1024*(calls.Load()+2) {
					t.Fatal("backend error traversal exceeded its bounded searches", visits)
				}
				if worker.Active() != 0 {
					t.Fatal("backend failure retained reservation owner")
				}
			})
		}
	}
}

// lostStartBackend applies a start, then loses its reply while the worker
// stops, as a network failure during shutdown does.
type lostStartBackend struct {
	jobs.Backend
	stop func()
}

func (b lostStartBackend) JobStart(ctx context.Context, key jobs.Key, owner jobs.Ownership) (uint32, error) {
	if _, err := b.Backend.JobStart(ctx, key, owner); err != nil {
		return 0, err
	}
	b.stop()
	<-ctx.Done()
	return 0, ctx.Err()
}

// A start whose outcome is unknown when the worker stops is refunded, so the
// handler that never ran does not consume an attempt.
func TestWorkerRefundsAStartLostDuringShutdown(t *testing.T) {
	var handled atomic.Int32
	handler := func(context.Context, payload) error {
		handled.Add(1)
		return nil
	}
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), handler)
	id := f.enqueue(t)
	declaration, err := f.definition.Declare(handler)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), "default")
	config.Concurrency, config.PollInterval, config.OperationTimeout = 1, time.Millisecond, 5*time.Second
	var worker *jobs.Worker
	stopped := make(chan error, 1)
	backend := lostStartBackend{Backend: f.backend, stop: func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stopped <- worker.Stop(ctx)
		}()
	}}
	worker, err = jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	record := waitRecord(t, f, id, jobs.Waiting)
	if record.Attempts != 0 || handled.Load() != 0 {
		t.Fatal("a lost start consumed an attempt", record.Attempts, handled.Load())
	}
}
