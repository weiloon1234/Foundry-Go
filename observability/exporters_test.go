package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/observability"
)

func TestTraceQueueSamplingAndErrorQueueAreIndependent(t *testing.T) {
	for _, sampled := range []bool{true, false} {
		config := observability.DefaultConfig()
		config.SampleTraces = sampled
		config.TraceQueue, config.ErrorQueue, config.TraceConcurrency, config.ReporterConcurrency = 1, 2, 1, 1
		traces, reports := make(chan observability.Entry, 2), make(chan observability.ErrorReport, 2)
		config.TraceExporters = []observability.TraceExporter{func(_ context.Context, entry observability.Entry) error { traces <- entry; return nil }}
		recorder, err := observability.New(config, func(_ context.Context, report observability.ErrorReport) error { reports <- report; return nil })
		if err != nil {
			t.Fatal(err)
		}
		config.TraceExporters[0] = func(context.Context, observability.Entry) error {
			t.Error("recorder retained caller-owned exporter slice")
			return nil
		}
		for range 2 {
			_, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.Job, Name: "delivery"})
			if err != nil {
				t.Fatal(err)
			}
			span.End(observability.Result{Outcome: observability.Failed})
		}
		snapshot := recorder.Snapshot()
		if snapshot.PendingReports != 2 || snapshot.DroppedReports != 0 {
			t.Fatal("trace traffic displaced error reports")
		}
		if sampled && (snapshot.PendingTraces != 1 || snapshot.DroppedTraces != 1) || !sampled && (snapshot.PendingTraces != 0 || snapshot.DroppedTraces != 0) {
			t.Fatal("incorrect trace sampling or bound", snapshot)
		}
		done := make(chan error, 1)
		go func() { done <- recorder.Run(t.Context()) }()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		if err := recorder.Ready(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		if err := recorder.Close(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if len(reports) != 2 || sampled && len(traces) != 1 || !sampled && len(traces) != 0 {
			t.Fatal("exporters lost accepted work")
		}
		if sampled {
			entry := <-traces
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			var restored observability.Entry
			if err := json.Unmarshal(data, &restored); err != nil || restored.TraceID != entry.TraceID || restored.ParentID != entry.ParentID || restored.SpanID != entry.SpanID {
				t.Fatal("typed trace snapshot did not round-trip", err)
			}
		}
	}
}

func TestTraceExporterFailureAndLifetimeRemainOwned(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	config := observability.DefaultConfig()
	config.TraceConcurrency = 1
	config.TraceTimeout = 20 * time.Millisecond
	config.TraceExporters = []observability.TraceExporter{
		func(context.Context, observability.Entry) error { runtime.Goexit(); return nil },
		func(context.Context, observability.Entry) error { close(entered); <-release; return nil },
	}
	recorder, err := observability.New(config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- recorder.Run(t.Context()) }()
	_, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.Schedule, Name: "rollup"})
	if err != nil {
		t.Fatal(err)
	}
	span.End(observability.Result{})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("failed exporter prevented independent exporter")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	err = recorder.Close(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close abandoned context-ignoring exporter", err)
	}
	select {
	case <-recorder.Done():
		t.Fatal("done preceded exporter exit")
	default:
	}
	resume()
	ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if recorder.Snapshot().TraceExportFailures != 2 {
		t.Fatal("exporter failures were not isolated/accounted")
	}
}

func TestTraceSampleRatioIsValidatedAndApplied(t *testing.T) {
	for _, ratio := range []float64{-0.5, 1.5} {
		config := observability.DefaultConfig()
		config.TraceSampleRatio = ratio
		if _, err := observability.New(config); err == nil {
			t.Fatal("invalid sampling ratio accepted", ratio)
		}
	}
	for _, test := range []struct {
		ratio    float64
		min, max uint64
	}{{0, 400, 400}, {1, 400, 400}, {1e-12, 0, 0}, {0.5, 120, 280}} {
		config := observability.DefaultConfig()
		config.TraceSampleRatio = test.ratio
		config.TraceQueue = 4096
		config.TraceExporters = []observability.TraceExporter{func(context.Context, observability.Entry) error { return nil }}
		recorder, err := observability.New(config)
		if err != nil {
			t.Fatal(err)
		}
		for range 400 {
			work, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.Job, Name: "sampled"})
			if err != nil {
				t.Fatal(err)
			}
			// A child always follows its root's decision.
			_, child, err := recorder.Start(work, observability.Operation{Kind: observability.Resource, Name: "child"})
			if err != nil {
				t.Fatal(err)
			}
			child.End(observability.Result{})
			span.End(observability.Result{})
		}
		pending := uint64(recorder.Snapshot().PendingTraces)
		if pending%2 != 0 || pending/2 < test.min || pending/2 > test.max {
			t.Fatal("sampling ratio or trace agreement failed", test.ratio, pending)
		}
		if err := recorder.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTraceBatchExportersReceiveBoundedOwnedBatches(t *testing.T) {
	config := observability.DefaultConfig()
	config.TraceBatchSize = 3
	config.TraceConcurrency = 1
	var mu sync.Mutex
	var sizes []int
	calls := 0
	config.TraceBatchExporters = []observability.TraceBatchExporter{
		func(_ context.Context, batch []observability.Entry) error {
			mu.Lock()
			defer mu.Unlock()
			sizes = append(sizes, len(batch))
			batch[0].Operation.Name = "mutated"
			return nil
		},
		func(_ context.Context, batch []observability.Entry) error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if batch[0].Operation.Name == "mutated" {
				t.Error("batch exporters shared one slice")
			}
			return errors.New("collector unavailable")
		},
	}
	invalid := config
	invalid.TraceBatchSize = 513
	if _, err := observability.New(invalid); err == nil {
		t.Fatal("oversized batch accepted")
	}
	recorder, err := observability.New(config)
	if err != nil {
		t.Fatal(err)
	}
	for range 7 {
		_, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.Job, Name: "batched"})
		if err != nil {
			t.Fatal(err)
		}
		span.End(observability.Result{})
	}
	done := make(chan error, 1)
	go func() { done <- recorder.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := recorder.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	total := 0
	for _, size := range sizes {
		if size < 1 || size > 3 {
			t.Fatal("batch exceeded its bound", sizes)
		}
		total += size
	}
	if total != 7 || recorder.Snapshot().TraceExportFailures != uint64(calls) {
		t.Fatal("batch export lost entries or failure accounting", sizes, calls)
	}
}
