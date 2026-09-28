package observability_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestObservationsShareTraceWithoutUnboundedLabels(t *testing.T) {
	config := observability.DefaultConfig()
	config.MaxRecent = 2
	recorder, err := observability.New(config)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := tracing.Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "vendor=private-vendor-data")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tracing.WithContext(t.Context(), remote)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: "private-request-id"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = attribution.WithContext(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	work, parent, err := recorder.Start(ctx, observability.Operation{Kind: observability.HTTP, Name: "users.list"})
	if err != nil {
		t.Fatal(err)
	}
	_, child, err := recorder.Start(work, observability.Operation{Kind: observability.Resource, Name: "database.primary"})
	if err != nil {
		t.Fatal(err)
	}
	child.End(observability.Result{})
	var concurrent sync.WaitGroup
	for range 8 {
		concurrent.Go(func() { parent.End(observability.Result{Status: 200}) })
	}
	concurrent.Wait()
	snapshot := recorder.Snapshot()
	if snapshot.Completed != 2 || snapshot.Active != 0 || len(snapshot.Recent) != 2 || len(snapshot.Metrics) != 2 {
		t.Fatal("span completion duplicated or lost ownership", snapshot)
	}
	if snapshot.Recent[0].TraceID != remote.TraceID() || snapshot.Recent[0].ParentID != snapshot.Recent[1].SpanID || snapshot.Recent[1].ParentID != remote.SpanID() {
		t.Fatal("nested trace lineage was lost")
	}
	snapshot.Recent[0].Operation.Name = "changed"
	snapshot.Metrics[0].Count = 99
	if recorder.Snapshot().Recent[0].Operation.Name == "changed" || recorder.Snapshot().Metrics[0].Count == 99 {
		t.Fatal("snapshot aliases recorder")
	}
	var metrics bytes.Buffer
	if err := recorder.WritePrometheus(&metrics); err != nil {
		t.Fatal(err)
	}
	text := metrics.String()
	for _, private := range []string{"private-request-id", "private-vendor-data", remote.TraceID().String()} {
		if strings.Contains(text, private) {
			t.Fatal("correlation data entered metric labels")
		}
	}
	if !strings.Contains(text, "# TYPE foundry_operation_duration_seconds histogram\n") || !strings.Contains(text, "le=\"+Inf\"") || !strings.HasSuffix(text, "\n") {
		t.Fatal("invalid Prometheus exposition")
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestObservationBoundsPreserveTotalsAndDropAccounting(t *testing.T) {
	config := observability.DefaultConfig()
	config.MaxSeries, config.MaxRecent, config.MaxActive, config.ErrorQueue = 1, 1, 1, 1
	recorder, err := observability.New(config, func(context.Context, observability.ErrorReport) error { t.Error("unstarted reporter ran"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	op := observability.Operation{Kind: observability.Job, Name: "orders.deliver"}
	_, first, err := recorder.Start(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	if _, span, err := recorder.Start(t.Context(), op); !errors.Is(err, fault.Conflict) || span != nil {
		t.Fatal("span admission exceeded its bound", err)
	}
	first.End(observability.Result{Outcome: observability.Failed})
	_, second, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.Job, Name: "orders.archive"})
	if err != nil {
		t.Fatal(err)
	}
	second.End(observability.Result{Outcome: observability.Panicked})
	snapshot := recorder.Snapshot()
	if snapshot.Completed != 2 || snapshot.Failures != 2 || snapshot.DroppedSpans != 1 || snapshot.DroppedSeries != 1 || snapshot.DroppedReports != 1 || len(snapshot.Recent) != 1 || snapshot.Recent[0].Operation.Name != "orders.archive" {
		t.Fatal("bounded recorder lost overflow accounting", snapshot)
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if recorder.Snapshot().DroppedReports != 2 {
		t.Fatal("unstarted reporter queue was silently discarded")
	}
	if _, _, err := recorder.Start(t.Context(), op); !errors.Is(err, fault.Closed) {
		t.Fatal("closed recorder admitted span", err)
	}
}

func TestReporterShutdownOwnsContextIgnoringCallback(t *testing.T) {
	config := observability.DefaultConfig()
	config.ReporterConcurrency, config.ReporterTimeout = 1, 20*time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	recorder, err := observability.New(config, func(context.Context, observability.ErrorReport) error { close(entered); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- recorder.Run(t.Context()) }()
	_, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.CLI, Name: "example"})
	if err != nil {
		t.Fatal(err)
	}
	span.End(observability.Result{Outcome: observability.Failed})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reporter did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	err = recorder.Close(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close abandoned live reporter", err)
	}
	select {
	case <-recorder.Done():
		t.Fatal("reporter done closed before callback exit")
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
	if recorder.Snapshot().ReporterFailures != 1 {
		t.Fatal("ignored reporter deadline was not reported")
	}
}

type hostileError struct{}

func (hostileError) Error() string { panic("private error formatting") }
func (hostileError) Is(error) bool { runtime.Goexit(); return false }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestObservationFailuresRemainIsolated(t *testing.T) {
	for _, test := range []struct {
		err  error
		want observability.Outcome
	}{
		{nil, observability.Succeeded}, {context.Canceled, observability.Cancelled}, {context.DeadlineExceeded, observability.TimedOut}, {hostileError{}, observability.Panicked}, {errors.New("private domain failure"), observability.Failed},
	} {
		if got := observability.OutcomeFor(test.err); got != test.want {
			t.Fatal("unsafe or incorrect outcome classification", got)
		}
	}
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.WritePrometheus(shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short exposition write accepted", err)
	}
	_, span, err := recorder.Start(t.Context(), observability.Operation{Kind: observability.HTTP, Name: "users.list"})
	if err != nil {
		t.Fatal(err)
	}
	span.End(observability.Result{Status: 999})
	if recorder.Snapshot().Active != 0 || recorder.Snapshot().DroppedSpans != 1 {
		t.Fatal("invalid result retained span ownership")
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
