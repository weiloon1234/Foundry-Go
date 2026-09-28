package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestWorkerPausesReservationAndRestoresOnlyOwnedCorrelation(t *testing.T) {
	type privateKey struct{}
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := recorder.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	var calls atomic.Int32
	contexts := make(chan context.Context, 2)
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 1
	f := newWorkerFixture(t, policy, func(ctx context.Context, _ payload) error {
		contexts <- ctx
		if calls.Add(1) == 1 {
			return recorder.Gate().Set(true)
		}
		return errors.New("private job failure")
	})
	origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: "queued-request"})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.definition.Dispatch(dispatch, f.dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.definition.Dispatch(dispatch, f.dispatcher, payload{Labels: map[string]string{}}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := tracing.WithContext(observability.WithContext(context.WithValue(t.Context(), privateKey{}, "private"), recorder), kernel)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- f.worker.Run(parent) }()
	waitRecord(t, f, first.ID, jobs.Succeeded)
	waitRecord(t, f, second.ID, jobs.Waiting)
	select {
	case <-time.After(30 * time.Millisecond):
	}
	if calls.Load() != 1 {
		t.Fatal("paused worker reserved queued work")
	}
	if err := recorder.Gate().Set(false); err != nil {
		t.Fatal(err)
	}
	waitRecord(t, f, second.ID, jobs.Failed)
	if err := f.worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx := <-contexts
		if ctx.Value(privateKey{}) != nil || observability.FromContext(ctx) != recorder || attribution.FromContext(ctx).Request().ID != "queued-request" || tracing.FromContext(ctx).IsZero() || tracing.FromContext(ctx).TraceID() == kernel.TraceID() {
			t.Fatal("worker lost declared correlation or inherited kernel context")
		}
	}
	snapshot := recorder.Snapshot()
	if snapshot.Completed != 2 || snapshot.Failures != 1 || snapshot.Active != 0 {
		t.Fatal("job outcome observations incorrect", snapshot)
	}
}
