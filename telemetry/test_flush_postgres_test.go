package telemetry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/telemetrytest"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func admitTestFlushObservations(t *testing.T, e *Engine) {
	t.Helper()
	for range 2 {
		req := request()
		req.Result.Status = 500
		e.collector.ObserveRequest(t.Context(), req)
	}
	if err := e.collector.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	if err := e.RecordBrowser(t.Context(), browserReport()); err != nil {
		t.Fatal(err)
	}
}

func assertTestFlushObservations(t *testing.T, e *Engine) {
	t.Helper()
	traffic, err := e.Reader().Traffic(t.Context(), TrafficQuery{Window: Window{Minutes: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if traffic.Summary.Count != 2 || traffic.Summary.Failed != 2 || len(traffic.Items) != 1 {
		t.Fatalf("flushed traffic: %+v", traffic)
	}
	for _, source := range []Source{SourceBrowser, SourceBackend} {
		page, err := e.Reader().Issues(t.Context(), IssueQuery{Source: source, Page: orm.Page(1, 10)})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Count != 1 {
			t.Fatalf("flushed %s issue counts: %+v", source, page)
		}
		events, err := e.Reader().Events(t.Context(), EventQuery{Fingerprint: page.Items[0].Fingerprint, Page: orm.Page(1, 10)})
		if err != nil {
			t.Fatal(err)
		}
		if events.Total != 1 || len(events.Items) != 1 {
			t.Fatalf("flushed %s event counts: %+v", source, events)
		}
	}
}

func TestPostgresTestFlushPersistsPendingFIFOAndEvaluatesIncidents(t *testing.T) {
	e, db, schema, wall := postgresEngine(t)
	wall.set(retentionNow(t, db, schema).UTC())
	e.storage.config.Alerts = incidentConfig(RuleHTTPErrors).Alerts
	admitTestFlushObservations(t, e)
	writer := e.storage.transact
	boom := errors.New("test flush unavailable")
	e.storage.transact = func(context.Context, func(*database.Tx) error, ...database.TxOptions) error { return boom }
	if err := e.Flush(t.Context(), telemetrytest.Grant()); !errors.Is(err, boom) {
		t.Fatalf("failed flush: %v", err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{}) {
		t.Fatalf("failed flush published state: %+v", got)
	}
	e.storage.transact = writer
	// One fresh capture plus the pending head are two sustain observations, but
	// their cumulative histograms must replace rather than add to one another.
	if err := e.Flush(t.Context(), telemetrytest.Grant()); err != nil {
		t.Fatal(err)
	}
	assertTestFlushObservations(t, e)
	requireIncident(t, db, schema, e.collector, RuleHTTPErrors, AlertStateOpen, 1)
	if got := countTelemetry(t, db, schema).receipts; got != 2 {
		t.Fatalf("FIFO drain receipts = %d, want 2", got)
	}
	if err := e.Flush(t.Context(), telemetrytest.Grant()); err != nil {
		t.Fatal(err)
	}
	assertTestFlushObservations(t, e)
	requireIncident(t, db, schema, e.collector, RuleHTTPErrors, AlertStateOpen, 1)
	e.mu.Lock()
	started := e.started
	e.mu.Unlock()
	if started {
		t.Fatal("test flush started a loop")
	}
}

func TestPostgresTestFlushConcurrentWithLoopDoesNotDuplicateCounts(t *testing.T) {
	e, db, schema, wall := postgresEngine(t)
	wall.set(retentionNow(t, db, schema).UTC())
	e.storage.config.Alerts = incidentConfig(RuleHTTPErrors).Alerts
	admitTestFlushObservations(t, e)
	firstWrite, secondCapture, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var captures atomic.Int32
	if err := e.deps.Recorder.RegisterCollector("test.flush.overlap", func(*observability.MetricWriter) {
		if captures.Add(1) == 2 {
			close(secondCapture)
		}
	}); err != nil {
		t.Fatal(err)
	}
	writer := e.storage.transact
	first := true // The storage gate serializes transaction callbacks.
	e.storage.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return writer(ctx, func(tx *database.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			if first {
				first = false
				close(firstWrite)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}, options...)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	loopDone := make(chan error, 1)
	go func() { loopDone <- e.run(ctx, ticks) }()
	waitModuleSignal(t, firstWrite, "loop did not enter its first flush")
	flushed := make(chan error, 1)
	go func() { flushed <- e.Flush(t.Context(), telemetrytest.Grant()) }()
	waitModuleSignal(t, secondCapture, "test flush did not capture alongside loop storage")
	unblock()
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-loopDone; err != nil {
		t.Fatal(err)
	}
	assertTestFlushObservations(t, e)
	requireIncident(t, db, schema, e.collector, RuleHTTPErrors, AlertStateOpen, 1)
	if got := countTelemetry(t, db, schema).receipts; got != 2 {
		t.Fatalf("concurrent flush receipts = %d, want 2", got)
	}
}

func TestPostgresTestFlushOwnershipAndCloseAdmission(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	if err := e.deps.Recorder.RegisterCollector("test.flush.ownership", func(*observability.MetricWriter) {
		enterOnce.Do(func() { close(entered) })
		<-release // Callback ownership survives a caller's shutdown deadline.
	}); err != nil {
		t.Fatal(err)
	}
	flushed := make(chan error, 1)
	go func() { flushed <- e.Flush(t.Context(), telemetrytest.Grant()) }()
	waitModuleSignal(t, entered, "test flush did not enter capture")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := e.Close(ctx); !errors.Is(err, fault.Timeout) {
		t.Fatalf("Close while capture owns work: %v", err)
	}
	if err := e.Flush(t.Context(), telemetrytest.Grant()); !errors.Is(err, fault.Closed) {
		t.Fatalf("flush admitted after Close began: %v", err)
	}
	select {
	case <-e.Done():
		t.Fatal("Close released dependencies under admitted capture")
	default:
	}
	unblock()
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.Flush(t.Context(), telemetrytest.Grant()); !errors.Is(err, fault.Closed) {
		t.Fatalf("flush admitted after Close completed: %v", err)
	}
	if got := countTelemetry(t, db, schema).receipts; got != 2 {
		t.Fatalf("admitted and final capture receipts = %d, want 2", got)
	}
}

func TestPostgresTestFlushBudgetLeavesPendingBatch(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	e.config.DrainTimeout = 20 * time.Millisecond
	writer := e.storage.transact
	e.storage.transact = func(ctx context.Context, _ func(*database.Tx) error, _ ...database.TxOptions) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := e.Flush(t.Context(), telemetrytest.Grant()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush budget: %v", err)
	}
	e.collector.mu.Lock()
	pending, failures := len(e.collector.pending), e.collector.failures
	e.collector.mu.Unlock()
	if pending != 1 || failures != 0 {
		t.Fatalf("interrupted flush: pending=%d storage failures=%d", pending, failures)
	}
	e.storage.transact = writer
	e.config.DrainTimeout = engineConfig().DrainTimeout
	if err := e.Flush(t.Context(), telemetrytest.Grant()); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema).receipts; got != 2 {
		t.Fatalf("retry lost pending receipt: %d", got)
	}
}

func TestPostgresTestFlushRequiresTokenAndContext(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	for _, engine := range []*Engine{nil, {}, e} {
		if err := engine.Flush(t.Context(), telemetrytest.Token{}); !errors.Is(err, fault.Invalid) {
			t.Fatalf("zero token accepted: %v", err)
		}
	}
	if err := e.Flush(nil, telemetrytest.Grant()); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil context accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := e.Flush(ctx, telemetrytest.Grant()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{}) {
		t.Fatalf("rejected flush wrote rows: %+v", got)
	}
}
