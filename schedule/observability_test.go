package schedule_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestSchedulerMaintenanceKeepsBoundedCatchUpAndOwnedTracing(t *testing.T) {
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
	kernel, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := tracing.WithContext(observability.WithContext(context.WithValue(t.Context(), privateKey{}, "private"), recorder), kernel)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	contexts := make(chan context.Context, 2)
	f := newFixture(t, every(t, "reports", func(ctx context.Context, _ schedule.Invocation) error {
		contexts <- ctx
		if calls.Add(1) == 1 {
			return recorder.Gate().Set(true)
		}
		return nil
	}))
	f.startContext(t, parent)
	f.advance(time.Minute)
	waitFor(t, func() bool {
		history := f.scheduler.Snapshot().History
		return len(history) == 1 && history[0].State == schedule.Succeeded
	})
	f.advance(time.Minute)
	select {
	case <-time.After(30 * time.Millisecond):
	}
	if calls.Load() != 1 || len(f.scheduler.Snapshot().History) != 1 {
		t.Fatal("maintenance admitted another occurrence")
	}
	if err := recorder.Gate().Set(false); err != nil {
		t.Fatal(err)
	}
	f.wake <- struct{}{}
	waitFor(t, func() bool {
		history := f.scheduler.Snapshot().History
		return len(history) == 2 && history[1].State == schedule.Succeeded
	})
	if err := f.scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx := <-contexts
		if observability.FromContext(ctx) != recorder || ctx.Value(privateKey{}) != nil || tracing.FromContext(ctx).IsZero() || tracing.FromContext(ctx).TraceID() == kernel.TraceID() {
			t.Fatal("schedule lost declared tracing or inherited kernel values")
		}
	}
	if snapshot := recorder.Snapshot(); snapshot.Completed != 2 || snapshot.Active != 0 || snapshot.Failures != 0 {
		t.Fatal("schedule outcome recording failed", snapshot)
	}
}
