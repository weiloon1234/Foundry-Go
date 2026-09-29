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
	idle, cancelled chan struct{}
	entered, exited sync.Once
	failure         error
	renewals        atomic.Int32
	handed          atomic.Bool
}

func (b *leaseAdmissionBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	// Hand out the single job once. After its lease really expires the queue
	// could legitimately redeliver it; this fixture keeps other loops idle.
	if !b.handed.Load() {
		found, err := b.Backend.JobReserve(ctx, key, owner, ttl)
		if err != nil || found.IsSet() {
			b.handed.Store(found.IsSet())
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

func (b *leaseAdmissionBackend) JobRenew(ctx context.Context, _ jobs.Key, _ jobs.Ownership, _ time.Duration) (jobs.LeaseStatus, error) {
	select {
	case <-b.idle:
		b.renewals.Add(1)
		return jobs.LeaseStatus{}, b.failure
	case <-ctx.Done():
		return jobs.LeaseStatus{}, ctx.Err()
	}
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
	cancelled, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var cancelledAt atomic.Int64
	declaration, err := f.definition.Declare(func(ctx context.Context, _ payload) error {
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
	backend := &leaseAdmissionBackend{Backend: f.backend, idle: make(chan struct{}), cancelled: make(chan struct{}), failure: failure}
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
