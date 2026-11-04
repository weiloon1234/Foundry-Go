package jobs_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/value"
)

type leaseAdmissionBackend struct {
	jobs.Backend
	idle, running, cancelled chan struct{}
	entered, exited          sync.Once
	failure                  error
	renewals                 atomic.Int32
	handed                   atomic.Bool
}

func (b *leaseAdmissionBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	// Hand out the single job once. After its lease really expires the queue
	// could legitimately redeliver it; this fixture keeps other loops idle.
	if !b.handed.Load() {
		found, err := b.Backend.JobReserve(ctx, key, owner, ttl)
		if err != nil || found.IsSet() {
			// Another loop's failed reservation must not undo a handed-out job.
			if found.IsSet() {
				b.handed.Store(true)
			}
			return found, err
		}
	}
	b.entered.Do(func() { close(b.idle) })
	<-ctx.Done()
	// Each reservation is bounded by OperationTimeout; only cancellation by the
	// worker itself means the loop was stopped.
	if errors.Is(ctx.Err(), context.Canceled) {
		b.exited.Do(func() { close(b.cancelled) })
	}
	return value.Optional[jobs.Reservation]{}, ctx.Err()
}

// JobRenew reports the scenario only once the handler runs and the other loop is
// idle. A loss any earlier would correctly keep the handler from starting; see
// TestLeaseLostBeforeStartDoesNotRunHandler.
func (b *leaseAdmissionBackend) JobRenew(context.Context, jobs.Key, jobs.Ownership, time.Duration) (jobs.LeaseStatus, error) {
	for _, gate := range []<-chan struct{}{b.idle, b.running} {
		select {
		case <-gate:
		default:
			return jobs.LeaseStatus{Owned: true}, nil
		}
	}
	b.renewals.Add(1)
	return jobs.LeaseStatus{}, b.failure
}

// Lease loss cancels only the affected handler. A renewal error leaves
// ownership unknown, so the heartbeat keeps retrying until the lease would
// really have expired. Other reservation loops keep running throughout.
func TestLeaseLossCancelsOnlyItsHandlerAndKeepsOtherLoops(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		failure error
	}{{"lost", nil}, {"renewal-error", errors.New("renewal failed")}} {
		t.Run(scenario.name, func(t *testing.T) { checkLeaseLossAdmission(t, scenario.failure) })
	}
}

func checkLeaseLossAdmission(t *testing.T, failure error) {
	t.Helper()
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	running, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	var cancelledAt atomic.Int64
	declaration, err := f.definition.Declare(func(ctx context.Context, _ payload) error {
		close(running)
		<-ctx.Done()
		cancelledAt.Store(time.Now().UnixNano())
		close(cancelled)
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend := &leaseAdmissionBackend{Backend: f.backend, idle: make(chan struct{}), running: running, cancelled: make(chan struct{}), failure: failure}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency = 2
	config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout = 300*time.Millisecond, 5*time.Millisecond, 50*time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	f.enqueue(t)
	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- worker.Run(t.Context()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error("stopped worker reported a failure", err)
			}
		case <-ctx.Done():
			t.Error("worker did not finish")
		}
	})
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("lease loss did not cancel handler")
	}
	elapsed := time.Duration(cancelledAt.Load() - started.UnixNano())
	if failure != nil && (elapsed < config.LeaseDuration || backend.renewals.Load() < 2) {
		t.Fatal("renewal error cancelled the handler before the lease could expire", elapsed, backend.renewals.Load())
	}
	select {
	case <-backend.cancelled:
		t.Fatal("lease loss stopped another reservation loop")
	case <-time.After(50 * time.Millisecond):
	}
	if worker.Active() != 1 {
		t.Fatal("lease loss released a live handler")
	}
	select {
	case <-worker.Done():
		t.Fatal("worker abandoned handler")
	default:
	}
	release <- struct{}{}
	if err := worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// leaseLostBeforeStartBackend reports the lease as lost once preparation has begun,
// and records any start.
type leaseLostBeforeStartBackend struct {
	jobs.Backend
	preparing <-chan struct{}
	started   atomic.Bool
}

func (b *leaseLostBeforeStartBackend) JobRenew(context.Context, jobs.Key, jobs.Ownership, time.Duration) (jobs.LeaseStatus, error) {
	select {
	case <-b.preparing:
		return jobs.LeaseStatus{}, nil
	default:
		return jobs.LeaseStatus{Owned: true}, nil
	}
}

func (b *leaseLostBeforeStartBackend) JobStart(ctx context.Context, key jobs.Key, proof jobs.Ownership) (uint32, error) {
	b.started.Store(true)
	return b.Backend.JobStart(ctx, key, proof)
}

// Another worker may own the job once the lease is lost. A loss seen during
// preparation must stop the attempt before it starts or consumes any budget.
func TestLeaseLostBeforeStartDoesNotRunHandler(t *testing.T) {
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	var ran atomic.Bool
	var entering, leaving sync.Once
	preparing, admitted := make(chan struct{}), make(chan struct{})
	declaration, err := f.definition.DeclareWith(func(context.Context, payload) error {
		ran.Store(true)
		return nil
	}, jobs.HandlerOptions[payload]{Admission: func(ctx context.Context, _ payload) (time.Duration, error) {
		entering.Do(func() { close(preparing) })
		<-ctx.Done()
		leaving.Do(func() { close(admitted) })
		return 0, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	backend := &leaseLostBeforeStartBackend{Backend: f.backend, preparing: preparing}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency = 1
	config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout = 300*time.Millisecond, 5*time.Millisecond, 50*time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	f.enqueue(t)
	done := make(chan error, 1)
	go func() { done <- worker.Run(t.Context()) }()
	deadline := time.After(3 * time.Second)
	select {
	case <-admitted:
	case <-deadline:
		t.Fatal("lease loss did not reach preparation")
	}
	for worker.Active() != 0 {
		select {
		case <-deadline:
			t.Fatal("the attempt did not end after its lease was lost")
		case <-time.After(time.Millisecond):
		}
	}
	if err := worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if backend.started.Load() || ran.Load() {
		t.Fatal("attempt started after its lease was lost")
	}
}
