package telemetry

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func postgresEngine(t *testing.T) (*Engine, *database.DB, string, *testClock) {
	t.Helper()
	db, migrations, schema := telemetryMigrationFixture(t)
	if _, err := migrations.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	c, source := newCollector(t, collectorConfig())
	recorder, err := observability.New(observability.DefaultConfig(), c.Report)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(c, Dependencies{DB: db, Schema: schema, Recorder: recorder, Clock: source}, engineConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Close(ctx)
		<-e.Done()
		_ = recorder.Close(ctx)
		<-recorder.Done()
	})
	return e, db, schema, source
}

func TestEngineRunIDIsTheOwnedCollectorRun(t *testing.T) {
	e, _, _, _ := postgresEngine(t)
	var zero model.ID[Run]
	if e.RunID() != e.collector.RunID() || e.RunID() == zero {
		t.Fatal("engine run identity differs from its collector run")
	}
	for _, engine := range []*Engine{nil, {}} {
		if engine.RunID() != zero {
			t.Fatal("uninitialized engine returned a run identity")
		}
	}
}

func TestEngineRunCancellationLeavesFinalCaptureToClose(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	e.collector.ObserveRequest(t.Context(), request())
	if err := e.collector.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := e.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{}) {
		t.Fatalf("Run cancellation finalized: %+v", got)
	}
	select {
	case <-e.Done():
		t.Fatal("Run cancellation released ownership before Close")
	default:
	}
	if err := e.Run(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatalf("second Run: %v", err)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{runs: 1, receipts: 1, metrics: 2, issues: 1, events: 1}) {
		t.Fatalf("final capture: %+v", got)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema).receipts; got != 1 {
		t.Fatal("Close captured more than once")
	}
	select {
	case <-e.deps.Recorder.Done():
		t.Fatal("engine closed borrowed recorder")
	default:
	}
	if _, err := e.Prune(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatalf("prune after Close: %v", err)
	}
	if _, err := e.Reader().Overview(t.Context(), OverviewQuery{Window: Window{Minutes: 1}}); !errors.Is(err, fault.Closed) {
		t.Fatalf("read after Close: %v", err)
	}
	if _, err := New(e.collector, e.deps, e.config); !errors.Is(err, fault.Conflict) {
		t.Fatalf("second collector owner: %v", err)
	}
}

func TestEngineCloseTimeoutRetainsSamplerAndCountsLateReports(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	if err := e.deps.Recorder.RegisterCollector("telemetry.test.blocked", func(*observability.MetricWriter) { close(entered); <-release }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- e.Close(ctx) }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, fault.Timeout) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Close wait: %v", err)
	}
	select {
	case <-e.Done():
		t.Fatal("Done closed under sampler")
	default:
	}
	if err := e.collector.Report(t.Context(), backendReport()); err != nil {
		t.Fatal(err)
	}
	if err := e.RecordBrowser(t.Context(), browserReport()); err != nil {
		t.Fatal(err)
	}
	e.collector.mu.Lock()
	dropped := e.collector.dropped
	pending := len(e.collector.issues)
	e.collector.mu.Unlock()
	if dropped != 2 || pending != 0 {
		t.Fatalf("late report reopened intake: dropped=%d issues=%d", dropped, pending)
	}
	if got := countTelemetry(t, db, schema); got != (telemetryCounts{}) {
		t.Fatal("database changed under blocked final capture")
	}
	releaseOnce.Do(func() { close(release) })
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema).receipts; got != 1 {
		t.Fatal("final capture did not complete after callback exit")
	}
	e.collector.mu.Lock()
	before := e.collector.dropped
	e.collector.mu.Unlock()
	_ = e.collector.Report(t.Context(), backendReport())
	e.collector.mu.Lock()
	after := e.collector.dropped
	e.collector.mu.Unlock()
	if after != before+1 {
		t.Fatal("reports after Done were not counted as shutdown loss")
	}
}

func TestEngineStorageFailureDoesNotExitCriticalLoop(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	writer := e.storage.transact
	failed, interrupted := make(chan struct{}), make(chan struct{})
	calls := 0
	e.storage.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		calls++
		switch calls {
		case 1: // First capture's flush fails; the loop then prunes.
			close(failed)
			return errors.New("injected storage failure")
		case 4: // Second tick retries the head, then starts the next batch.
			return writer(ctx, func(tx *database.Tx) error {
				if err := fn(tx); err != nil {
					return err
				}
				close(interrupted)
				<-ctx.Done()
				return ctx.Err()
			}, options...)
		default:
			return writer(ctx, fn, options...)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- e.run(ctx, ticks) }()
	select {
	case ticks <- time.Now():
	case err := <-done:
		t.Fatalf("Run exited before its first tick: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not accept its first tick")
	}
	waitModuleSignal(t, failed, "first flush did not fail")
	// An unbuffered tick is accepted only when the previous cycle finished.
	select {
	case ticks <- time.Now():
	case err := <-done:
		t.Fatalf("storage failure ended Run: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not accept its next tick")
	}
	waitModuleSignal(t, interrupted, "next FIFO batch did not enter its transaction")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.collector.mu.Lock()
	failures, pending := e.collector.failures, len(e.collector.pending)
	var retained batch
	if pending == 1 {
		retained = e.collector.pending[0]
	}
	e.collector.mu.Unlock()
	if failures != 1 || pending != 1 {
		t.Fatalf("canceled flush: storage failures=%d pending=%d", failures, pending)
	}
	if got := countTelemetry(t, db, schema).receipts; got != 1 {
		t.Fatalf("committed FIFO head receipts = %d", got)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := countTelemetry(t, db, schema).receipts; got != 3 {
		t.Fatalf("retry and final capture receipts = %d", got)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.RequireKey(t.Context(), tx, flushes.Table, receiptKey(retained.id))
		return err
	})
	e.collector.mu.Lock()
	failures = e.collector.failures
	e.collector.mu.Unlock()
	if failures != 1 {
		t.Fatalf("storage failures = %d", failures)
	}
}

func TestEnginePruneCancellationDoesNotCountStorageFailure(t *testing.T) {
	e, _, _, _ := postgresEngine(t)
	writer := e.storage.transact
	entered := make(chan struct{})
	first := true
	e.storage.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		if !first {
			return writer(ctx, fn, options...)
		}
		first = false
		return writer(ctx, func(tx *database.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}, options...)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.Prune(ctx); done <- err }()
	waitModuleSignal(t, entered, "prune did not enter its transaction")
	cancel()
	if err := <-done; !storageInterrupted(ctx, err) {
		t.Fatalf("prune cancellation = %v", err)
	}
	if _, err := e.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.collector.mu.Lock()
	failures := e.collector.failures
	e.collector.mu.Unlock()
	if failures != 0 {
		t.Fatalf("storage failures = %d", failures)
	}
}

type failingCaptureClock struct {
	mode atomic.Int32
	now  time.Time
}

func (c *failingCaptureClock) Now() time.Time {
	switch c.mode.Load() {
	case 1:
		panic("private clock payload")
	case 2:
		runtime.Goexit()
	}
	return c.now
}

func TestEngineFinalCaptureContainsClockPanicAndGoexit(t *testing.T) {
	for _, mode := range []int32{1, 2} {
		t.Run(map[int32]string{1: "panic", 2: "goexit"}[mode], func(t *testing.T) {
			e, db, schema, clock := postgresEngine(t)
			source := &failingCaptureClock{now: clock.Now()}
			e.collector.clock = source
			_ = e.collector.Report(t.Context(), backendReport())
			source.mode.Store(mode)
			if err := e.Close(t.Context()); !errors.Is(err, fault.Panicked) {
				t.Fatalf("capture callback failure: %v", err)
			}
			select {
			case <-e.Done():
			default:
				t.Fatal("callback failure stranded finalization")
			}
			if countTelemetry(t, db, schema) != (telemetryCounts{}) {
				t.Fatal("failed capture persisted partial snapshot")
			}
			e.collector.mu.Lock()
			dropped, failures := e.collector.dropped, e.collector.failures
			e.collector.mu.Unlock()
			if dropped != 1 || failures != 1 {
				t.Fatalf("failed capture accounting: dropped=%d failures=%d", dropped, failures)
			}
		})
	}
}

func TestEngineCaptureRecordsUnavailableReadinessAndFrozenPoolUsage(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	if err := e.capture(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.collector.mu.Lock()
	b := e.collector.pending[0]
	e.collector.mu.Unlock()
	if !b.runtime.Health.Ready.IsNull() {
		t.Fatal("nil readiness became healthy")
	}
	if b.poolUsage < 0 || b.poolUsage > 1 {
		t.Fatal("invalid captured pool usage")
	}
	if err := e.drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		run, err := orm.RequireKey(t.Context(), tx, runs.Table, e.storage.runKey())
		if err != nil {
			return err
		}
		runtime, err := decodePayload(run.Runtime, maxRuntimePayloadBytes)
		if err != nil {
			return err
		}
		if !runtime.Health.Ready.IsNull() {
			t.Error("unavailable readiness was persisted as a boolean")
		}
		return nil
	})
	// Framework clock values remain the captured observation timestamp.
	at, err := temporal.NewDateTime(e.collector.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if b.at != at {
		t.Fatal("capture did not use framework clock precision")
	}
}

func TestEngineRecordBrowserPreservesAdmissionAndShutdownLoss(t *testing.T) {
	e, db, schema, _ := postgresEngine(t)
	invalid := browserReport()
	invalid.Release = "unknown"
	if err := e.RecordBrowser(t.Context(), invalid); !errors.Is(err, fault.Invalid) {
		t.Fatalf("invalid browser report: %v", err)
	}
	report := browserReport()
	if err := e.RecordBrowser(t.Context(), report); err != nil {
		t.Fatal(err)
	}
	report.Frames[0].File = "private-mutation"
	// Browser admission owns one quarter of the existing event bound.
	for range e.collector.config.MaxEvents / 4 {
		if err := e.RecordBrowser(t.Context(), browserReport()); err != nil {
			t.Fatal(err)
		}
	}
	e.collector.mu.Lock()
	admitted, dropped := len(e.collector.issues), e.collector.dropped
	var file string
	if admitted > 0 {
		file = e.collector.issues[0].Detail.Frames[0].File
	}
	e.collector.mu.Unlock()
	if admitted != e.collector.config.MaxEvents/4 || dropped != 1 || file != "admin/_assets/app.js" {
		t.Fatalf("browser admission: admitted=%d dropped=%d file=%q", admitted, dropped, file)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := countTelemetry(t, db, schema)
	if before.issues != 1 || before.events == 0 {
		t.Fatalf("browser reports were not drained: %+v", before)
	}
	e.collector.mu.Lock()
	dropped = e.collector.dropped
	e.collector.mu.Unlock()
	// Sealing takes precedence over validation, exactly as on Collector.
	if err := e.RecordBrowser(t.Context(), invalid); err != nil {
		t.Fatalf("late browser report: %v", err)
	}
	e.collector.mu.Lock()
	lateLoss, pending, intake := e.collector.dropped, len(e.collector.pending), len(e.collector.issues)
	e.collector.mu.Unlock()
	if lateLoss != dropped+1 || pending != 0 || intake != 0 {
		t.Fatal("late browser report did not remain counted shutdown loss")
	}
	if after := countTelemetry(t, db, schema); after != before {
		t.Fatal("browser admission wrote after Close")
	}
}
