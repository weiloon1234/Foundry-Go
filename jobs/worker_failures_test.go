package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func runWorker(t *testing.T, f workerFixture) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f.worker.Run(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := f.worker.Stop(ctx); err != nil {
			t.Error(err)
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("worker did not return")
		}
	})
	return done
}
func TestWorkerIsolatesPanicAndGoexit(t *testing.T) {
	for _, kind := range []string{"panic", "goexit"} {
		t.Run(kind, func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			policy.Attempts = 1
			f := newWorkerFixture(t, policy, func(context.Context, payload) error {
				if kind == "panic" {
					panic("private payload")
				}
				runtime.Goexit()
				return nil
			})
			id := f.enqueue(t)
			runWorker(t, f)
			record := waitRecord(t, f, id, jobs.Failed)
			if record.Attempts != 1 || record.History[len(record.History)-1].Reason != jobs.HandlerPanicked {
				t.Fatal(record)
			}
		})
	}
}
func TestWorkerCancellationWaitsForHandler(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), func(ctx context.Context, _ payload) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	})
	id := f.enqueue(t)
	runWorker(t, f)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	if ok, err := f.definition.Cancel(t.Context(), f.dispatcher, id, ""); err != nil || !ok {
		t.Fatal(ok, err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("not cancelled")
	}
	record := waitRecord(t, f, id, jobs.Running)
	if !record.CancellationRequested || f.worker.Active() != 1 {
		t.Fatal("handler ownership abandoned")
	}
	release <- struct{}{}
	waitRecord(t, f, id, jobs.Cancelled)
}
func TestWorkerSelfShutdownAndSavedContext(t *testing.T) {
	var f workerFixture
	contextResult := make(chan context.Context, 1)
	f = newWorkerFixture(t, jobs.DefaultPolicy("default"), func(ctx context.Context, _ payload) error {
		if err := f.worker.Stop(ctx); !errors.Is(err, fault.Cycle) {
			return errors.New("self shutdown was not rejected")
		}
		if _, ok := f.definition.CurrentID(ctx); !ok {
			return errors.New("typed execution identity missing")
		}
		contextResult <- ctx
		return nil
	})
	id := f.enqueue(t)
	runWorker(t, f)
	waitRecord(t, f, id, jobs.Succeeded)
	ctx := <-contextResult
	if _, ok := jobs.Current(ctx); ok {
		t.Fatal("saved context retained active execution")
	}
}
func TestAdmissionDenialsDoNotConsumeAttempts(t *testing.T) {
	var admissions atomic.Int32
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 1
	f := newWorkerFixture(t, policy, func(context.Context, payload) error { return nil }, jobs.HandlerOptions[payload]{Admission: func(context.Context, payload) (time.Duration, error) {
		if admissions.Add(1) < 3 {
			return time.Millisecond, nil
		}
		return 0, nil
	}})
	id := f.enqueue(t)
	runWorker(t, f)
	record := waitRecord(t, f, id, jobs.Succeeded)
	if record.Attempts != 1 || admissions.Load() != 3 {
		t.Fatalf("attempts=%d admissions=%d", record.Attempts, admissions.Load())
	}
}
func TestAdmissionTimeoutConsumesRetryBudget(t *testing.T) {
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 1
	policy.Timeout = 5 * time.Millisecond
	f := newWorkerFixture(t, policy, func(context.Context, payload) error { t.Error("timed-out admission executed handler"); return nil }, jobs.HandlerOptions[payload]{Admission: func(ctx context.Context, _ payload) (time.Duration, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}})
	id := f.enqueue(t)
	runWorker(t, f)
	record := waitRecord(t, f, id, jobs.Failed)
	if record.Attempts != 1 || record.History[len(record.History)-1].Reason != jobs.TimedOut {
		t.Fatal(record)
	}
}
func TestMiddlewareUnwindsFailuresAndIsolatesCallbacks(t *testing.T) {
	calls := make(chan string, 10)
	hook := func(label string, fail bool) jobs.Handler[payload] {
		return func(context.Context, payload) error {
			calls <- label
			if fail {
				panic("private")
			}
			return nil
		}
	}
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 1
	f := newWorkerFixture(t, policy, hook("handler", false), jobs.HandlerOptions[payload]{Middleware: []jobs.Middleware[payload]{
		{Before: hook("before-a", false), After: hook("after-a", false), Failed: func(context.Context, payload, error) error { calls <- "failed-a"; return nil }},
		{Before: hook("before-b", false), After: hook("after-b", true), Failed: func(context.Context, payload, error) error { calls <- "failed-b"; return nil }},
	}})
	id := f.enqueue(t)
	runWorker(t, f)
	waitRecord(t, f, id, jobs.Failed)
	close(calls)
	var actual []string
	for v := range calls {
		actual = append(actual, v)
	}
	if !slices.Equal(actual, []string{"before-a", "before-b", "handler", "after-b", "after-a", "failed-b", "failed-a"}) {
		t.Fatal(actual)
	}
}

func TestWorkerRejectsUnknownVersionsAndInvalidPayloads(t *testing.T) {
	for _, kind := range []string{"unknown-version", "invalid-payload"} {
		t.Run(kind, func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			f := newWorkerFixture(t, policy, func(context.Context, payload) error { t.Error("invalid delivery invoked handler"); return nil })
			pending, err := f.definition.Capture(t.Context(), payload{Labels: map[string]string{}}, jobs.Options[payload]{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := pending.Envelope().MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			reason := jobs.Unregistered
			if kind == "unknown-version" {
				wire["version"] = json.RawMessage("2")
			} else {
				wire["payload"] = json.RawMessage(`{"unexpected":true}`)
				reason = jobs.PayloadInvalid
			}
			data, err = json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := jobs.DecodeEnvelope(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.backend.JobEnqueue(t.Context(), f.key, envelope); err != nil {
				t.Fatal(err)
			}
			runWorker(t, f)
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				found, err := f.backend.JobInspect(t.Context(), f.key, envelope.ID())
				if err != nil {
					t.Fatal(err)
				}
				if r, ok := found.Get(); ok && r.State == jobs.Failed {
					if r.History[len(r.History)-1].Reason != reason {
						t.Fatal(r.History)
					}
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("poison delivery was not retained as failure")
		})
	}
}
