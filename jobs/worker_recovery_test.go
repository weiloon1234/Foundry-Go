package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/value"
)

type recoveringWorkerBackend struct {
	jobs.Backend
	calls   chan time.Time
	healthy atomic.Bool
}

func (b *recoveringWorkerBackend) JobWakeup(key jobs.Key) <-chan struct{} {
	return b.Backend.(jobs.WakeBackend).JobWakeup(key)
}

func (b *recoveringWorkerBackend) JobReserve(ctx context.Context, key jobs.Key, owner lease.Owner, ttl time.Duration) (value.Optional[jobs.Reservation], error) {
	if !b.healthy.Load() {
		select {
		case b.calls <- time.Now():
		case <-ctx.Done():
		}
		return value.Optional[jobs.Reservation]{}, errors.New("backend unavailable")
	}
	return b.Backend.JobReserve(ctx, key, owner, ttl)
}

func TestWorkerFailurePauseLocalEnqueueAndMinimum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ran := make(chan time.Time, 2)
		handler := func(context.Context, payload) error { ran <- time.Now(); return nil }
		f := newWorkerFixture(t, jobs.DefaultPolicy("default"), handler)
		declaration, err := f.definition.Declare(handler)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := jobs.NewRegistry(declaration)
		if err != nil {
			t.Fatal(err)
		}
		config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
		config.Concurrency, config.PollInterval, config.FailureBackoff = 1, 100*time.Millisecond, 30*time.Second
		backend := &recoveringWorkerBackend{Backend: f.backend, calls: make(chan time.Time)}
		worker, err := jobs.NewWorker(backend, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- worker.Run(context.Background()) }()
		defer func() {
			if err := worker.Stop(t.Context()); err != nil {
				t.Error(err)
			}
			if err := <-done; err != nil {
				t.Error(err)
			}
		}()
		var failedAt time.Time
		for range 7 {
			failedAt = <-backend.calls
		}
		// A wake during an ongoing outage cannot bypass the minimum pause.
		f.enqueue(t)
		synctest.Wait()
		time.Sleep(config.PollInterval / 2)
		select {
		case <-backend.calls:
			t.Fatal("wake bypassed the minimum failure pause")
		default:
		}
		wokenAt := <-backend.calls
		if wokenAt.Sub(failedAt) != config.PollInterval {
			t.Fatal("local enqueue did not shorten failure backoff", wokenAt.Sub(failedAt))
		}
		// Waking did not reset failures: with no second wake, the next delay
		// remains in the upper half of the eighth exponential interval.
		failedAt = <-backend.calls
		if elapsed := failedAt.Sub(wokenAt); elapsed < 64*config.PollInterval || elapsed > 128*config.PollInterval {
			t.Fatal("wake reset the failure count", elapsed)
		}
		backend.healthy.Store(true)
		f.enqueue(t)
		synctest.Wait()
		time.Sleep(config.PollInterval / 2)
		select {
		case <-ran:
			t.Fatal("recovered worker bypassed minimum failure pause")
		default:
		}
		select {
		case at := <-ran:
			if at.Sub(failedAt) != config.PollInterval {
				t.Fatal("local enqueue did not wake recovered worker promptly", at.Sub(failedAt))
			}
		case <-time.After(config.PollInterval):
			t.Fatal("worker remained in failure backoff after local enqueue")
		}
	})
}
