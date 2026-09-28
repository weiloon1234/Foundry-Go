package jobs_test

import (
	"context"
	"runtime"
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
}

func (b failingWorkerBackend) fail() error {
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

func TestWorkerBackendCallbacksAndErrorInspectionRetainShutdownOwnership(t *testing.T) {
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
				config.Concurrency, config.PollInterval = 1, time.Millisecond
				config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout = 150*time.Millisecond, 15*time.Millisecond, 30*time.Millisecond
				cycle := &cyclicJobError{}
				backend := failingWorkerBackend{Backend: f.backend, operation: operation, mode: mode}
				if mode == "cycle" {
					backend.failure = cycle
				}
				worker, err := jobs.NewWorker(backend, registry, config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					if err := worker.Stop(ctx); err != nil {
						t.Error(err)
					}
				})
				done := make(chan error, 1)
				go func() { done <- worker.Run(t.Context()) }()
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("backend failure accepted")
					}
				case <-time.After(time.Second):
					t.Fatal("backend failure stranded worker")
				}
				if cycle.visits.Load() > 512 {
					t.Fatal("backend error traversal exceeded its bounded searches", cycle.visits.Load())
				}
				if worker.Active() != 0 {
					t.Fatal("backend failure retained reservation owner")
				}
				select {
				case <-worker.Done():
				default:
					t.Fatal("worker returned before owned cleanup")
				}
			})
		}
	}
}
