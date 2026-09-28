package foundation_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestApplicationOwnsReporterUntilActualExit(t *testing.T) {
	entered, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	config := observability.DefaultConfig()
	config.ReporterConcurrency = 1
	recorder, err := observability.New(config, func(_ context.Context, report observability.ErrorReport) error {
		if report.Entry.Operation.Name != "app.resource" {
			t.Error("unexpected report", report.Entry.Operation)
		}
		close(entered)
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := foundation.NewBuilder(foundation.WithObservability(recorder), foundation.WithShutdownTimeout(20*time.Millisecond)).Register(foundation.Module{
		Name: "app", OnBoot: func(ctx context.Context, runtime *foundation.Runtime) error {
			if runtime.Observability() != recorder || observability.FromContext(ctx) != recorder || tracing.FromContext(ctx).IsZero() {
				return errors.New("missing runtime observation context")
			}
			return runtime.OnShutdown("resource", func(context.Context) error { close(cleaned); return errors.New("private failure") })
		},
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	err = app.Shutdown(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || app.State() != foundation.Stopping {
		t.Fatal("shutdown abandoned a live reporter", err, app.State())
	}
	await(t, cleaned)
	await(t, entered)
	if recorder.Gate().Mode() != maintenance.Draining {
		t.Fatal("shutdown did not seal admission")
	}
	select {
	case <-app.Done():
		t.Fatal("application done preceded reporter exit")
	default:
	}
	resume()
	await(t, app.Done())
	if recorder.Snapshot().Active != 0 || !recorder.Snapshot().Closing {
		t.Fatal("application retained observation ownership")
	}
}

func TestRecorderClaimAndShutdownBeforeStartup(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	app, err := foundation.NewBuilder(foundation.WithObservability(recorder)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foundation.NewBuilder(foundation.WithObservability(recorder)).Build(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal("two apps acquired the same recorder", err)
	}
	if app.Observability() != recorder {
		t.Fatal("app lost its recorder")
	}
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	await(t, recorder.Done())
	if err := app.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}

func TestEveryKernelObservesConstructionRunAndLegacyProviderNames(t *testing.T) {
	for _, kind := range []foundation.KernelKind{foundation.HTTP, foundation.CLI, foundation.Worker, foundation.Scheduler, foundation.WebSocket} {
		t.Run(string(kind), func(t *testing.T) {
			recorder, err := observability.New(observability.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			app, err := foundation.NewBuilder(foundation.WithObservability(recorder)).Register(foundation.Module{
				Name: "LegacyProvider", OnRegister: func(registrar *foundation.Registrar) error {
					return registrar.Kernel(kind, func(*foundation.Runtime) (foundation.Kernel, error) {
						return foundation.KernelFunc(func(ctx context.Context) error {
							if tracing.FromContext(ctx).IsZero() || observability.FromContext(ctx) != recorder {
								return errors.New("kernel lost shared observation context")
							}
							return nil
						}), nil
					})
				}, OnBoot: func(context.Context, *foundation.Runtime) error { return nil },
			}).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := app.Run(t.Context(), kind); err != nil {
				t.Fatal(err)
			}
			snapshot := recorder.Snapshot()
			if snapshot.Completed != 3 || snapshot.DroppedSpans != 0 || snapshot.Active != 0 {
				t.Fatal("lifecycle observations missing", snapshot)
			}
			if !strings.HasPrefix(string(snapshot.Recent[0].Operation.Name), "declared.") {
				t.Fatal("legacy provider name was not bounded")
			}
		})
	}
}
