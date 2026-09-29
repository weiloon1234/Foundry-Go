package foundation_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

func kernelModule(kind foundation.KernelKind, run func(context.Context) error) foundation.Module {
	return foundation.Module{Name: foundation.ProviderID("kernel-" + string(kind)), OnRegister: func(r *foundation.Registrar) error {
		return r.Kernel(kind, func(*foundation.Runtime) (foundation.Kernel, error) { return foundation.KernelFunc(run), nil })
	}}
}

func TestGracefulServiceStopReturnsNilAndCommandInterruptionDoesNot(t *testing.T) {
	for _, test := range []struct {
		kind        foundation.KernelKind
		interrupted bool
	}{{foundation.HTTP, false}, {foundation.Worker, false}, {foundation.CLI, true}} {
		t.Run(string(test.kind), func(t *testing.T) {
			entered := make(chan struct{})
			app := build(t, kernelModule(test.kind, func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- app.Run(ctx, test.kind) }()
			await(t, entered)
			cancel()
			err := await(t, result)
			if test.interrupted != errors.Is(err, context.Canceled) || !test.interrupted && err != nil {
				t.Fatalf("graceful stop classification: %v", err)
			}
			if app.State() != foundation.Stopped {
				t.Fatal("run returned before shutdown completed")
			}
		})
	}
}

func TestKernelFailureIsReportedOnce(t *testing.T) {
	failure := errors.New("kernel exploded")
	app := build(t, kernelModule(foundation.CLI, func(context.Context) error { return failure }))
	err := app.Run(t.Context(), foundation.CLI)
	if !errors.Is(err, failure) || strings.Count(err.Error(), failure.Error()) != 1 {
		t.Fatalf("kernel failure must be reported exactly once: %v", err)
	}
	bootFailure := errors.New("boot exploded")
	app = build(t, kernelModule(foundation.HTTP, func(context.Context) error { return nil }), foundation.Module{Name: "broken", OnBoot: func(context.Context, *foundation.Runtime) error { return bootFailure }})
	err = app.Run(t.Context(), foundation.HTTP)
	if !errors.Is(err, bootFailure) || strings.Count(err.Error(), bootFailure.Error()) != 1 {
		t.Fatalf("boot failure must be reported exactly once: %v", err)
	}
}

func TestStopDelayKeepsKernelsServingWhileStopping(t *testing.T) {
	entered := make(chan struct{})
	var kernelStopped atomic.Int64
	app, err := foundation.NewBuilder(foundation.WithShutdownTimeout(2*time.Second), foundation.WithStopDelay(150*time.Millisecond)).Register(kernelModule(foundation.HTTP, func(ctx context.Context) error {
		if maintenance.FromContext(ctx) == nil {
			return errors.New("kernel context lost the maintenance gate")
		}
		close(entered)
		<-ctx.Done()
		kernelStopped.Store(time.Now().UnixNano())
		return ctx.Err()
	})).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- app.Run(ctx, foundation.HTTP) }()
	await(t, entered)
	requested := time.Now()
	cancel()
	time.Sleep(50 * time.Millisecond)
	if app.State() != foundation.Stopping || kernelStopped.Load() != 0 || app.Maintenance().Mode() != maintenance.Serving {
		t.Fatal("stop delay must report stopping while the kernel keeps serving", app.State(), app.Maintenance().Mode())
	}
	if err := await(t, result); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Duration(kernelStopped.Load() - requested.UnixNano()); elapsed < 140*time.Millisecond {
		t.Fatal("kernel stopped before the configured delay", elapsed)
	}
	if app.Maintenance().Mode() != maintenance.Draining {
		t.Fatal("admission was not closed after the delay")
	}
}

func TestShutdownBudgetCoversKernelDrainAndCleanup(t *testing.T) {
	budget := 400 * time.Millisecond
	var remaining atomic.Int64
	app, err := foundation.NewBuilder(foundation.WithShutdownTimeout(budget)).Register(
		kernelModule(foundation.HTTP, func(ctx context.Context) error { <-ctx.Done(); time.Sleep(150 * time.Millisecond); return ctx.Err() }),
		foundation.Module{Name: "resource", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
			return r.OnShutdown("pool", func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("cleanup has no deadline")
				}
				remaining.Store(int64(time.Until(deadline)))
				return nil
			})
		}},
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- app.Run(ctx, foundation.HTTP) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := await(t, result); err != nil {
		t.Fatalf("drain within budget must not report a pending shutdown: %v", err)
	}
	if left := time.Duration(remaining.Load()); left <= 0 || left > budget-100*time.Millisecond {
		t.Fatal("cleanup did not share the remaining shutdown budget", left)
	}
}

func TestStartupTimeoutFailsBoot(t *testing.T) {
	for name, result := range map[string]func(context.Context) error{
		"cause": context.Cause,
		// Ordinary providers return ctx.Err(); the deadline is still reported
		// as a startup failure, not as an interrupt (CLI exit 130).
		"ctx.Err": func(ctx context.Context) error { return ctx.Err() },
	} {
		t.Run(name, func(t *testing.T) {
			var output lockedBuffer
			app, err := foundation.NewBuilder(foundation.WithLogger(slog.New(slog.NewJSONHandler(&output, nil))), foundation.WithShutdownTimeout(time.Second), foundation.WithStartupTimeout(30*time.Millisecond)).Register(foundation.Module{Name: "slow", OnBoot: func(ctx context.Context, _ *foundation.Runtime) error {
				<-ctx.Done()
				return result(ctx)
			}}).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			err = app.Start(t.Context())
			if !errors.Is(err, fault.Timeout) || errors.Is(err, context.Canceled) || cli.Status(err) == cli.Interrupted {
				t.Fatal("startup deadline not reported as a failure", err, cli.Status(err))
			}
			if err := stop(t, app); !errors.Is(err, fault.Timeout) {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "application startup failed") {
				t.Fatal("startup deadline was not logged as a failure", output.String())
			}
		})
	}
}

func TestMaintenanceGateAlwaysExistsAndMustMatchRecorder(t *testing.T) {
	app := build(t, kernelModule(foundation.Worker, func(ctx context.Context) error {
		if maintenance.FromContext(ctx) == nil {
			return errors.New("missing gate")
		}
		return nil
	}))
	if app.Maintenance() == nil {
		t.Fatal("gate requires observability")
	}
	if err := app.Run(t.Context(), foundation.Worker); err != nil {
		t.Fatal(err)
	}
	if app.Maintenance().Mode() != maintenance.Draining {
		t.Fatal("shutdown did not drain the application gate")
	}
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foundation.NewBuilder(foundation.WithObservability(recorder), foundation.WithMaintenance(&maintenance.Gate{})).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("divergent gates accepted", err)
	}
	if _, err := foundation.NewBuilder(foundation.WithShutdownTimeout(time.Second), foundation.WithStopDelay(time.Second)).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("stop delay must be inside the shutdown budget", err)
	}
}

func TestRunKernelsSharesOneLifetime(t *testing.T) {
	var running atomic.Int32
	entered := make(chan struct{}, 2)
	serve := func(ctx context.Context) error {
		running.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		running.Add(-1)
		return ctx.Err()
	}
	app := build(t, kernelModule(foundation.HTTP, serve), kernelModule(foundation.Worker, serve), kernelModule(foundation.CLI, func(context.Context) error { return nil }))
	if err := app.RunKernels(t.Context(), foundation.HTTP, foundation.HTTP); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if err := app.RunKernels(t.Context(), foundation.HTTP, foundation.CLI); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- app.RunKernels(ctx, foundation.HTTP, foundation.Worker) }()
	await(t, entered)
	await(t, entered)
	cancel()
	if err := await(t, result); err != nil || running.Load() != 0 {
		t.Fatal("kernels did not stop together", err, running.Load())
	}
}

func TestLifecycleEventsAreLoggedWithoutErrorText(t *testing.T) {
	var output lockedBuffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	app, err := foundation.NewBuilder(foundation.WithLogger(logger), foundation.WithShutdownTimeout(time.Second)).Register(foundation.Module{Name: "resource", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		return r.OnShutdown("pool", func(context.Context) error { return errors.New("password=hunter2") })
	}}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = stop(t, app)
	text := output.String()
	for _, event := range []string{"application starting", "application ready", "application shutdown started", "application cleanup failed", "application stopped with failures", `"resource":"provider[resource]:pool"`} {
		if !strings.Contains(text, event) {
			t.Fatalf("missing lifecycle event %q in %s", event, text)
		}
	}
	if strings.Contains(text, "hunter2") {
		t.Fatal("lifecycle logs formatted an arbitrary error")
	}
}
