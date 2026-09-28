package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func TestPermanentFailureStopsRetryEvenAfterTimeout(t *testing.T) {
	if jobs.Permanent(nil) != nil {
		t.Fatal("nil marker changed success")
	}
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			policy.Timeout = 10 * time.Millisecond
			f := newWorkerFixture(t, policy, func(ctx context.Context, _ payload) error {
				if timeout {
					<-ctx.Done()
				}
				return errors.Join(errors.New("other"), fmt.Errorf("wrapped: %w", jobs.Permanent(errors.New("uncertain side effect"))))
			})
			id := f.enqueue(t)
			runWorker(t, f)
			record := waitRecord(t, f, id, jobs.Failed)
			if record.Attempts != 1 {
				t.Fatal("terminal failure retried", record.Attempts)
			}
		})
	}
}

type classifierError struct{ run func() }

func (classifierError) Error() string   { return "private error" }
func (e classifierError) Is(error) bool { e.run(); return false }
func TestWorkerOwnsErrorClassification(t *testing.T) {
	for _, kind := range []string{"panic", "goexit", "self-stop"} {
		t.Run(kind, func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			policy.Attempts = 1
			var f workerFixture
			f = newWorkerFixture(t, policy, func(ctx context.Context, _ payload) error {
				return classifierError{run: func() {
					switch kind {
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					case "self-stop":
						if !errors.Is(f.worker.Stop(ctx), fault.Cycle) {
							panic("classifier ownership was released")
						}
					}
				}}
			})
			id := f.enqueue(t)
			runWorker(t, f)
			record := waitRecord(t, f, id, jobs.Failed)
			want := jobs.HandlerPanicked
			if kind == "self-stop" {
				want = jobs.HandlerFailed
			}
			if record.History[len(record.History)-1].Reason != want {
				t.Fatal("classifier result", record.History)
			}
		})
	}
}

func TestPreventRetryOnlyAffectsLiveStartedAttempt(t *testing.T) {
	if jobs.PreventRetry(context.Background()) {
		t.Fatal("unowned execution modified")
	}
	var saved context.Context
	policy := jobs.DefaultPolicy("default")
	f := newWorkerFixture(t, policy, func(ctx context.Context, _ payload) error {
		saved = ctx
		if !jobs.PreventRetry(ctx) {
			t.Fatal("live attempt missing")
		}
		return classifierError{run: func() { runtime.Goexit() }}
	})
	id := f.enqueue(t)
	runWorker(t, f)
	record := waitRecord(t, f, id, jobs.Failed)
	if record.Attempts != 1 || jobs.PreventRetry(saved) {
		t.Fatal("retry guard lifetime incorrect")
	}
}
