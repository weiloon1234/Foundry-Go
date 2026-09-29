package observability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestObservedSuccessSurvivesLaterCancellation(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(observability.WithContext(t.Context(), recorder))
	err = observability.Observe(ctx, observability.Operation{Kind: observability.Job, Name: "orders.deliver"}, func(context.Context) error {
		// The work completed; its context ending afterwards must not rewrite it.
		cancel()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := observability.Observe(observability.WithContext(t.Context(), recorder), observability.Operation{Kind: observability.Job, Name: "orders.cancel"}, func(context.Context) error { return context.Canceled })
	if !errors.Is(failed, context.Canceled) {
		t.Fatal(failed)
	}
	snapshot := recorder.Snapshot()
	if len(snapshot.Recent) != 2 || snapshot.Recent[0].Result.Outcome != observability.Succeeded || snapshot.Recent[1].Result.Outcome != observability.Cancelled {
		t.Fatal("returned errors alone must decide outcomes", snapshot.Recent)
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledContextStillRecordsSpan(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	work, span, err := recorder.Start(ctx, observability.Operation{Kind: observability.HTTP, Name: "request"})
	if err != nil || span == nil || tracing.FromContext(work).IsZero() || !errors.Is(work.Err(), context.Canceled) {
		t.Fatal("cancelled operation lost its span or cancellation", err)
	}
	span.End(observability.Result{Outcome: observability.Cancelled})
	snapshot := recorder.Snapshot()
	if snapshot.Completed != 1 || snapshot.DroppedSpans != 0 || len(snapshot.Metrics) != 1 || snapshot.Metrics[0].Result.Outcome != observability.Cancelled {
		t.Fatal("cancelled operation was dropped from metrics", snapshot)
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
