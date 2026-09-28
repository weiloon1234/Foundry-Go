package jobs_test

import (
	"context"
	"errors"
	"sync"
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
}

func (b *leaseAdmissionBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	found, err := b.Backend.JobReserve(ctx, key, owner, ttl)
	if err != nil || found.IsSet() {
		return found, err
	}
	b.entered.Do(func() { close(b.idle) })
	<-ctx.Done()
	b.exited.Do(func() { close(b.cancelled) })
	return value.Optional[jobs.Reservation]{}, ctx.Err()
}

func (b *leaseAdmissionBackend) JobRenew(ctx context.Context, _ jobs.Key, _ jobs.Ownership, _ time.Duration) (jobs.LeaseStatus, error) {
	select {
	case <-b.idle:
		return jobs.LeaseStatus{}, b.failure
	case <-ctx.Done():
		return jobs.LeaseStatus{}, ctx.Err()
	}
}

func TestLeaseLossStopsOtherReservationsBeforeUncooperativeHandlerExits(t *testing.T) {
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
	declaration, err := f.definition.Declare(func(ctx context.Context, _ payload) error {
		<-ctx.Done()
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
	want := failure
	if want == nil {
		want = jobs.ErrOwnershipLost
	}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency = 2
	config.OperationTimeout = 2 * time.Second
	config.HeartbeatInterval = time.Millisecond
	worker, err := jobs.NewWorker(backend, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	f.enqueue(t)
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
			if !errors.Is(err, want) {
				t.Error("lost lease outcome missing", err)
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
	select {
	case <-backend.cancelled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("lease loss left another reservation loop running until the handler exited")
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
