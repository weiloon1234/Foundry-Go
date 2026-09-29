package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

// withWorker replaces the fixture worker with one using configure.
func withWorker(t *testing.T, f workerFixture, handler jobs.Handler[payload], configure func(*jobs.WorkerConfig)) workerFixture {
	t.Helper()
	declaration, err := f.definition.Declare(handler)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency, config.PollInterval = 1, time.Millisecond
	config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout = 150*time.Millisecond, 15*time.Millisecond, 30*time.Millisecond
	configure(&config)
	if f.worker, err = jobs.NewWorker(f.backend, registry, config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := f.worker.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return f
}

func TestDrainLetsAdmittedHandlerFinishAndStopsReserving(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	f = withWorker(t, f, func(ctx context.Context, _ payload) error {
		entered <- struct{}{}
		<-release
		return ctx.Err()
	}, func(c *jobs.WorkerConfig) { c.DrainTimeout = 3 * time.Second })
	first := f.enqueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.worker.Run(ctx) }()
	<-entered
	cancel()
	second := f.enqueue(t)
	// The heartbeat keeps the reservation alive past its original lease.
	time.Sleep(200 * time.Millisecond)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("drained worker did not stop")
	}
	if record := waitRecord(t, f, first, jobs.Succeeded); record.Attempts != 1 {
		t.Fatal("drained handler did not complete once", record.Attempts)
	}
	if record := waitRecord(t, f, second, jobs.Waiting); record.Attempts != 0 {
		t.Fatal("draining worker reserved new work")
	}
}

func TestDrainDeadlineCancelsAndRefundsInterruptedAttempt(t *testing.T) {
	entered := make(chan struct{}, 1)
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 1
	f := newWorkerFixture(t, policy, nil)
	f = withWorker(t, f, func(ctx context.Context, _ payload) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}, func(c *jobs.WorkerConfig) { c.DrainTimeout = 20 * time.Millisecond })
	id := f.enqueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.worker.Run(ctx) }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("drain deadline did not cancel the handler")
	}
	record := waitRecord(t, f, id, jobs.Waiting)
	if record.Attempts != 0 || record.History[len(record.History)-1].Reason != jobs.WorkerStopped {
		t.Fatalf("interrupted attempt consumed its only budget: %d %+v", record.Attempts, record.History)
	}
}

func TestHandlerReturningNilIsSucceededAfterStopOrPreventRetry(t *testing.T) {
	for _, mode := range []string{"stopped", "prevent-retry"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
			f = withWorker(t, f, func(ctx context.Context, _ payload) error {
				if mode == "prevent-retry" {
					jobs.PreventRetry(ctx)
					return nil
				}
				entered <- struct{}{}
				<-ctx.Done()
				// Side effects completed; the stop must not turn this into a retry.
				return nil
			}, func(c *jobs.WorkerConfig) { c.DrainTimeout = 0 })
			id := f.enqueue(t)
			done := make(chan error, 1)
			go func() { done <- f.worker.Run(context.Background()) }()
			if mode == "stopped" {
				<-entered
				if err := f.worker.Stop(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if record := waitRecord(t, f, id, jobs.Succeeded); record.Attempts != 1 {
				t.Fatal(record.Attempts)
			}
			_ = f.worker.Stop(t.Context())
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalEnqueueWakesIdleWorker(t *testing.T) {
	ran := make(chan struct{}, 1)
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), nil)
	f = withWorker(t, f, func(context.Context, payload) error { ran <- struct{}{}; return nil }, func(c *jobs.WorkerConfig) {
		c.PollInterval, c.MaxPollInterval = 10*time.Second, 10*time.Second
		c.LeaseDuration, c.HeartbeatInterval, c.OperationTimeout = 60*time.Second, time.Second, time.Second
	})
	runWorker(t, f)
	// Let the worker observe an empty queue and start its long idle wait.
	time.Sleep(50 * time.Millisecond)
	started := time.Now()
	f.enqueue(t)
	select {
	case <-ran:
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Fatal("enqueue did not wake the idle worker", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle worker was not woken")
	}
}

func TestRetryJitterIsProportionalToDelay(t *testing.T) {
	policy := jobs.DefaultPolicy("default")
	policy.Attempts, policy.Backoff, policy.Jitter = 2, []time.Duration{10 * time.Minute}, time.Millisecond
	f := newWorkerFixture(t, policy, func(context.Context, payload) error { return errors.New("fail") })
	id := f.enqueue(t)
	runWorker(t, f)
	record := waitRecord(t, f, id, jobs.Waiting)
	for record.Attempts == 0 {
		record = waitRecord(t, f, id, jobs.Waiting)
	}
	delay := record.AvailableAt.Sub(record.History[len(record.History)-1].At)
	// A fixed 1ms jitter would retry a burst of failures in lockstep; the spread
	// scales to a fifth of the delay.
	if delay < 10*time.Minute || delay > 12*time.Minute || delay <= 10*time.Minute+time.Millisecond {
		t.Fatal("retry delay was not spread proportionally", delay)
	}
}
