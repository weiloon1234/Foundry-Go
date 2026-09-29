package observability_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/observability"
)

// Saturate both exporter families with bounded concurrent domain work. Slow
// sinks must not hold span slots, grow queues/labels, or lose overflow totals.
func TestBoundedLoadWithBlockedExporters(t *testing.T) {
	const workers, perWorker = 32, 64
	release := make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	config := observability.DefaultConfig()
	config.MaxActive, config.MaxSeries, config.MaxRecent = 4, 8, 5
	config.ErrorQueue, config.TraceQueue = 3, 3
	config.ReporterTimeout, config.TraceTimeout = time.Minute, time.Minute
	config.TraceExporters = []observability.TraceExporter{func(context.Context, observability.Entry) error { <-release; return nil }}
	recorder, err := observability.New(config, func(context.Context, observability.ErrorReport) error { <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- recorder.Run(t.Context()) }()
	readyContext, readyCancel := context.WithTimeout(t.Context(), 3*time.Second)
	err = recorder.Ready(readyContext)
	readyCancel()
	if err != nil {
		t.Fatal("recorder did not start", err)
	}
	var accepted atomic.Uint64
	var work sync.WaitGroup
	for worker := range workers {
		work.Go(func() {
			for range perWorker {
				_, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.Job, Name: observability.Name(fmt.Sprintf("load.worker%d", worker))})
				if err != nil {
					continue
				}
				accepted.Add(1)
				span.End(observability.Result{Outcome: observability.Failed})
			}
		})
	}
	work.Wait()
	snapshot := recorder.Snapshot()
	if snapshot.Active != 0 || snapshot.Completed != accepted.Load() || snapshot.Failures != accepted.Load() || snapshot.Completed+snapshot.DroppedSpans != workers*perWorker {
		t.Fatal("concurrent span ownership or admission accounting failed", snapshot)
	}
	if len(snapshot.Metrics) > config.MaxSeries || len(snapshot.Recent) > config.MaxRecent || snapshot.PendingReports > config.ErrorQueue || snapshot.PendingTraces > config.TraceQueue || snapshot.DroppedReports == 0 || snapshot.DroppedTraces == 0 {
		t.Fatal("blocked sinks lost their finite budgets or overflow accounting", snapshot)
	}
	resume()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
}

// BenchmarkParallelSpans measures span admission and completion across many
// goroutines sharing a few hot series, the recorder's contended path.
func BenchmarkParallelSpans(b *testing.B) {
	config := observability.DefaultConfig()
	config.MaxRecent = 128
	recorder, err := observability.New(config)
	if err != nil {
		b.Fatal(err)
	}
	operations := []observability.Operation{{Kind: observability.HTTP, Name: "request"}, {Kind: observability.Job, Name: "deliver"}, {Kind: observability.Resource, Name: "database.primary"}}
	var next atomic.Uint64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		operation := operations[next.Add(1)%uint64(len(operations))]
		ctx := context.Background()
		for pb.Next() {
			_, span, err := recorder.Start(ctx, operation)
			if err != nil {
				b.Fatal(err)
			}
			span.End(observability.Result{})
		}
	})
	b.StopTimer()
	if err := recorder.Close(context.Background()); err != nil {
		b.Fatal(err)
	}
}
