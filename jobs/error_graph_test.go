package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
)

type cyclicJobError struct{ visits atomic.Int32 }

func (*cyclicJobError) Error() string { panic("private job error must not be formatted") }
func (e *cyclicJobError) Unwrap() error {
	// Keep a regression against the old traversal finite without accepting it.
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

func TestCyclicJobErrorsFinishRetriesAndReleaseWorker(t *testing.T) {
	for _, stage := range []string{"handler", "admission", "before", "failed", "permanent"} {
		t.Run(stage, func(t *testing.T) {
			failure := &cyclicJobError{}
			var recovered atomic.Bool
			policy := jobs.DefaultPolicy("default")
			policy.Attempts, policy.Backoff, policy.Jitter = 2, []time.Duration{0}, 0
			options := jobs.HandlerOptions[payload]{}
			handler := func(context.Context, payload) error {
				if recovered.Load() {
					return nil
				}
				if stage == "permanent" {
					return fmt.Errorf("terminal: %w", jobs.Permanent(failure))
				}
				if stage == "failed" {
					return errors.New("ordinary handler failure")
				}
				return failure
			}
			if stage == "admission" {
				options.Admission = func(context.Context, payload) (time.Duration, error) {
					if recovered.Load() {
						return 0, nil
					}
					return 0, failure
				}
			} else if stage == "before" {
				options.Middleware = []jobs.Middleware[payload]{{Before: handler}}
			} else if stage == "failed" {
				options.Middleware = []jobs.Middleware[payload]{{Failed: func(context.Context, payload, error) error { return failure }}}
			}
			f := newWorkerFixture(t, policy, handler, options)
			id := f.enqueue(t)
			runWorker(t, f)
			record := waitRecord(t, f, id, jobs.Failed)
			want := uint32(2)
			if stage == "permanent" {
				want = 1
			}
			if record.Attempts != want || record.History[len(record.History)-1].Reason != jobs.HandlerFailed {
				t.Fatal("cyclic failure lost retry policy or explicit terminal marker")
			}
			if failure.visits.Load() > 2048 {
				t.Fatal("job exceeded bounded searches across two attempts", failure.visits.Load())
			}
			recovered.Store(true)
			next := f.enqueue(t)
			if record := waitRecord(t, f, next, jobs.Succeeded); record.Attempts != 1 {
				t.Fatal("worker capacity was not reusable")
			}
			if err := f.worker.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if f.worker.Active() != 0 {
				t.Fatal("worker retained an execution after shutdown")
			}
		})
	}
}
