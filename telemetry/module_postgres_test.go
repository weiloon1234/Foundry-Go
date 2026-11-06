package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/observability"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresTelemetryModuleRetainsDependenciesPastShutdownDeadline(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	if _, err := telemetryMigrationRunner(t, db, scope.Schema()).Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	collector, source := newCollector(t, collectorConfig())
	metricEntered, releaseMetric := make(chan struct{}), make(chan struct{})
	lateEntered, releaseLate := make(chan struct{}), make(chan struct{})
	databaseClosed := make(chan struct{})
	var metricOnce, releaseMetricOnce, releaseLateOnce sync.Once
	unblockMetric := func() { releaseMetricOnce.Do(func() { close(releaseMetric) }) }
	unblockLate := func() { releaseLateOnce.Do(func() { close(releaseLate) }) }
	recorder, err := observability.New(observability.DefaultConfig(), func(ctx context.Context, report observability.ErrorReport) error {
		if err := collector.Report(ctx, report); err != nil {
			return err
		}
		if report.Entry.Operation.Name == "fixture.late" {
			close(lateEntered)
			<-releaseLate // Recorder must retain this admitted callback too.
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.RegisterCollector("fixture.blocked", func(w *observability.MetricWriter) {
		metricOnce.Do(func() { close(metricEntered) })
		<-releaseMetric // Deliberately ignores the engine's drain deadline.
		w.Counter("fixture_completed_total", "Fixture completed work.", 1)
	}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	databaseKey := foundation.NewKey[*database.DB]("fixture.telemetry-database")
	engineKey := foundation.NewKey[*Engine]("fixture.telemetry-engine")
	var engine *Engine
	owner := foundation.Module{Name: "fixture.database-owner", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, databaseKey, db)
	}, OnBoot: func(_ context.Context, runtime *foundation.Runtime) error {
		return runtime.OnShutdown("database", func(ctx context.Context) error {
			select {
			case <-engine.Done():
			default:
				return errors.New("database cleanup preceded telemetry ownership barrier")
			}
			select {
			case <-recorder.Done():
				return errors.New("recorder closed before borrowed resource cleanup")
			default:
			}
			closeErr := db.Close(ctx)
			<-db.Done()
			close(databaseClosed)
			// Foundation intentionally still admits reports while resources close.
			// This report must count as sealed telemetry loss, with no DB work.
			_, span, err := recorder.Start(context.Background(), observability.Operation{Kind: observability.Job, Name: "fixture.late"})
			if err != nil {
				return err
			}
			span.End(observability.Result{Outcome: observability.Failed})
			return closeErr
		})
	}}
	module := KernelModule("fixture.telemetry-owner", engineKey, nil, []foundation.ProviderID{owner.Name}, func(r foundation.Resolver) (*Engine, error) {
		ownedDB, err := foundation.Resolve(r, databaseKey)
		if err != nil {
			return nil, err
		}
		engine, err = New(collector, Dependencies{DB: ownedDB, Schema: scope.Schema(), Recorder: recorder, Clock: source, Logger: logger}, engineConfig())
		return engine, err
	})
	app, err := foundation.NewBuilder(foundation.WithClock(source), foundation.WithLogger(logger), foundation.WithObservability(recorder), foundation.WithShutdownTimeout(20*time.Millisecond)).Register(owner, module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unblockMetric()
		unblockLate()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	// Start without a selected kernel owns cleanup but starts no periodic loop.
	// The blocked collector below therefore belongs to the final capture.
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		shutdown <- app.Shutdown(ctx)
	}()
	waitModuleSignal(t, metricEntered, "final capture did not reach the metric callback")
	select {
	case err := <-shutdown:
		if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, fault.Timeout) {
			t.Fatal("shutdown did not retain timed-out ownership", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown wait ignored its deadline")
	}
	for _, state := range []struct {
		name string
		done <-chan struct{}
	}{{"engine", engine.Done()}, {"database", db.Done()}, {"recorder", recorder.Done()}, {"application", app.Done()}} {
		select {
		case <-state.done:
			t.Fatalf("%s released ownership under the blocked callback", state.name)
		default:
		}
	}
	if !db.Stats().Ready {
		t.Fatal("shutdown deadline closed the telemetry database early")
	}
	unblockMetric()
	waitModuleSignal(t, engine.Done(), "engine did not finish after its callback exited")
	waitModuleSignal(t, databaseClosed, "database did not close after telemetry completed")
	waitModuleSignal(t, lateEntered, "resource cleanup could not deliver its late report")
	collector.mu.Lock()
	dropped, pending, sealed := collector.dropped, len(collector.pending), collector.sealed
	collector.mu.Unlock()
	if dropped == 0 || pending != 0 || !sealed {
		t.Fatal("late report reopened telemetry intake or escaped loss accounting")
	}
	for _, state := range []struct {
		name string
		done <-chan struct{}
	}{{"recorder", recorder.Done()}, {"application", app.Done()}} {
		select {
		case <-state.done:
			t.Fatalf("%s completed under an admitted reporter callback", state.name)
		default:
		}
	}
	unblockLate()
	waitModuleSignal(t, recorder.Done(), "recorder did not drain after resources and callbacks")
	waitModuleSignal(t, app.Done(), "application did not finish after recorder drain")
	if err := app.Shutdown(t.Context()); !errors.Is(err, fault.Timeout) {
		t.Fatal("completed shutdown lost its original cleanup deadline", err)
	}
}

func waitModuleSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}
