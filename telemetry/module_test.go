package telemetry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func TestUnselectedTelemetryCloseWaitsWithoutCaptureOrWrite(t *testing.T) {
	collector, source := newCollector(t, collectorConfig())
	recorder, err := observability.New(observability.DefaultConfig(), collector.Report)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Close(context.Background()) })
	var captures, writes atomic.Int32
	if err := recorder.RegisterCollector("fixture.capture", func(*observability.MetricWriter) { captures.Add(1) }); err != nil {
		t.Fatal(err)
	}
	e, err := New(collector, Dependencies{DB: &database.DB{}, Schema: "fixture", Recorder: recorder, Clock: source}, engineConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.storage.transact = func(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
		writes.Add(1)
		return errors.New("unexpected storage")
	}
	key := foundation.NewKey[*Engine]("fixture.telemetry")
	app, err := foundation.NewBuilder(foundation.WithClock(source), foundation.WithObservability(recorder)).Register(KernelModule("fixture.telemetry", key, nil, nil, func(foundation.Resolver) (*Engine, error) { return e, nil })).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.operations.enter(); err != nil {
		t.Fatal(err)
	}
	left := false
	defer func() {
		if !left {
			e.operations.leave()
		}
	}()
	if err := collector.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := e.Close(ctx); !errors.Is(err, fault.Timeout) {
		t.Fatalf("admitted work did not retain close: %v", err)
	}
	select {
	case <-e.Done():
		t.Fatal("close released admitted work")
	default:
	}
	e.operations.leave()
	left = true
	wait, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := e.Close(wait); err != nil {
		t.Fatal(err)
	}
	if captures.Load() != 0 || writes.Load() != 0 {
		t.Fatal("unselected close captured or wrote")
	}
	if !collector.sealed || collector.dropped != 1 {
		t.Fatal("unselected close failed to seal/count discarded intake")
	}
	if _, err := e.Reader().Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 1}}); !errors.Is(err, fault.Closed) {
		t.Fatalf("reader after close = %v", err)
	}
}
