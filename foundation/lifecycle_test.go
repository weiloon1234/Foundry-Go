package foundation_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type journal struct {
	mu      sync.Mutex
	entries []string
}

func (j *journal) add(value string) { j.mu.Lock(); j.entries = append(j.entries, value); j.mu.Unlock() }
func (j *journal) all() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.entries...)
}

func build(t *testing.T, providers ...foundation.Provider) *foundation.App {
	t.Helper()
	app, err := foundation.NewBuilder(foundation.WithShutdownTimeout(time.Second)).Register(providers...).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx) // Individual failure tests inspect the error themselves.
		if app.State() != foundation.Stopped {
			t.Errorf("application leaked in state %s: %v", app.State(), app.PendingTasks())
		}
	})
	return app
}

func stop(t *testing.T, app *foundation.App) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return app.Shutdown(ctx)
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")
		var zero T
		return zero
	}
}

func TestProviderOrderingAndPartialBootCleanup(t *testing.T) {
	var log journal
	bootFailure := errors.New("boot failure")
	cleanupFailure := errors.New("cleanup failure")
	module := func(name foundation.ProviderID, requires []foundation.ProviderID, fail bool) foundation.Module {
		return foundation.Module{Name: name, Requires: requires,
			OnRegister: func(*foundation.Registrar) error { log.add("register:" + string(name)); return nil },
			OnBoot: func(_ context.Context, r *foundation.Runtime) error {
				log.add("boot:" + string(name))
				if err := r.OnShutdown("resource", func(ctx context.Context) error {
					if ctx.Err() != nil {
						return fmt.Errorf("cleanup received canceled context: %w", ctx.Err())
					}
					log.add("close:" + string(name))
					if name == "base" {
						return cleanupFailure
					}
					return nil
				}); err != nil {
					return err
				}
				if fail {
					return bootFailure
				}
				return nil
			},
		}
	}
	app := build(t, module("broken", []foundation.ProviderID{"base"}, true), module("base", nil, false), module("later", nil, false))
	if err := app.Start(t.Context()); !errors.Is(err, bootFailure) {
		t.Fatalf("startup cause lost: %v", err)
	}
	err := stop(t, app)
	if !errors.Is(err, bootFailure) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("shutdown lost failures: %v", err)
	}
	want := []string{"register:base", "register:broken", "register:later", "boot:base", "boot:broken", "close:broken", "close:base"}
	if got := log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	if err2 := stop(t, app); err2 != err {
		t.Fatal("repeated shutdown changed result")
	}
	if got := log.all(); !reflect.DeepEqual(got, want) {
		t.Fatal("cleanup ran more than once")
	}
}

func TestProviderGraphErrorsBeforeBoot(t *testing.T) {
	for _, test := range []struct {
		name    string
		modules []foundation.Provider
		code    fault.Code
	}{
		{"duplicate", []foundation.Provider{foundation.Module{Name: "a"}, foundation.Module{Name: "a"}}, fault.Duplicate},
		{"missing", []foundation.Provider{foundation.Module{Name: "a", Requires: []foundation.ProviderID{"b"}}}, fault.Missing},
		{"cycle", []foundation.Provider{foundation.Module{Name: "a", Requires: []foundation.ProviderID{"b"}}, foundation.Module{Name: "b", Requires: []foundation.ProviderID{"a"}}}, fault.Cycle},
		{"invalid", []foundation.Provider{foundation.Module{Name: "bad name"}}, fault.Invalid},
		{"nil", []foundation.Provider{(*foundation.Module)(nil)}, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := foundation.NewBuilder().Register(test.modules...).Build(t.Context())
			if !errors.Is(err, test.code) {
				t.Fatalf("got %v, want %s", err, test.code)
			}
		})
	}
}

func TestExplicitReplacementAndSingleUseBuilder(t *testing.T) {
	var calls atomic.Int32
	b := foundation.NewBuilder().Register(foundation.Module{Name: "a", OnBoot: func(context.Context, *foundation.Runtime) error { t.Error("old provider booted"); return nil }}).
		Replace(foundation.Module{Name: "a", OnBoot: func(context.Context, *foundation.Runtime) error { calls.Add(1); return nil }})
	app, err := b.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = stop(t, app); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("replacement did not boot")
	}
	if _, err = b.Build(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatalf("reused builder: %v", err)
	}
	_, err = foundation.NewBuilder().Replace(foundation.Module{Name: "missing"}).Build(t.Context())
	if !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
}

func TestConcurrentStartAndShutdownRunOnce(t *testing.T) {
	var boots, closes atomic.Int32
	app := build(t, foundation.Module{Name: "one", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		boots.Add(1)
		return r.OnShutdown("resource", func(context.Context) error { closes.Add(1); return nil })
	}})
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := app.Start(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for range 32 {
		wg.Go(func() {
			if err := stop(t, app); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if boots.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("boots=%d closes=%d", boots.Load(), closes.Load())
	}
}

func TestCanceledStartupAndPreparedShutdown(t *testing.T) {
	var boots atomic.Int32
	app := build(t, foundation.Module{Name: "a", OnBoot: func(context.Context, *foundation.Runtime) error { boots.Add(1); return nil }})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if boots.Load() != 0 || app.State() != foundation.Prepared {
		t.Fatal("canceled start booted resources")
	}
	if err := stop(t, app); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}

func TestShutdownWaitsForBootAndKeepsPartialResourcesOwned(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var closed atomic.Bool
	app := build(t, foundation.Module{Name: "slow", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		close(entered)
		<-release
		return r.OnShutdown("late-resource", func(context.Context) error { closed.Store(true); return nil })
	}})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan error, 1)
	go func() { started <- app.Start(ctx) }()
	await(t, entered)
	cancel()
	if err := await(t, started); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	deadline, done := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer done()
	if err := app.Shutdown(deadline); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, fault.Timeout) {
		t.Fatal(err)
	}
	if app.State() != foundation.Stopping || closed.Load() {
		t.Fatal("reported completion before boot exited")
	}
	once.Do(func() { close(release) })
	if err := stop(t, app); err != nil {
		t.Fatal(err)
	}
	if !closed.Load() {
		t.Fatal("partial resource was not cleaned up")
	}
}

func TestShutdownDoesNotCloseDependenciesUnderActiveTasks(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	var closed atomic.Bool
	var scope *foundation.Runtime
	app := build(t, foundation.Module{Name: "tasks", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		scope = r
		if err := r.OnShutdown("dependency", func(context.Context) error { closed.Store(true); return nil }); err != nil {
			return err
		}
		return r.Go("stubborn", func(context.Context) error {
			<-release
			if closed.Load() {
				return errors.New("dependency closed while task active")
			}
			return nil
		})
	}})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := scope.Go("stubborn", func(context.Context) error { return nil }); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := app.Shutdown(deadline); !errors.Is(err, fault.Timeout) {
		t.Fatal(err)
	}
	if closed.Load() || len(app.PendingTasks()) != 1 || app.State() != foundation.Stopping {
		t.Fatal("task ownership was lost")
	}
	if err := scope.Go("new", func(context.Context) error { return nil }); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := stop(t, app); err != nil {
		t.Fatal(err)
	}
	if !closed.Load() {
		t.Fatal("dependency cleanup missing")
	}
}

func TestCriticalTaskFailureAndPanicAreReported(t *testing.T) {
	for _, test := range []struct {
		name   string
		run    func(context.Context) error
		target error
	}{
		{"error", func(context.Context) error { return fault.Missing }, fault.Missing},
		{"panic", func(context.Context) error { panic("secret-must-not-leak") }, fault.Panicked},
		{"goexit", func(context.Context) error { runtime.Goexit(); return nil }, fault.Panicked},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := build(t, foundation.Module{Name: "critical", OnBoot: func(_ context.Context, r *foundation.Runtime) error { return r.Go("task", test.run) }})
			_ = app.Start(t.Context())
			await(t, app.Done())
			err := stop(t, app)
			if !errors.Is(err, test.target) || strings.Contains(err.Error(), "secret-must-not-leak") {
				t.Fatalf("unsafe/lost error: %v", err)
			}
		})
	}
}

func TestCancellationDoesNotHideJoinedFailures(t *testing.T) {
	failed := errors.New("real failure")
	app := build(t, foundation.Module{Name: "a", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		return r.Go("task", func(ctx context.Context) error { <-ctx.Done(); return errors.Join(context.Canceled, failed) })
	}})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := stop(t, app); !errors.Is(err, failed) {
		t.Fatalf("lost joined error: %v", err)
	}
}

func TestKernelsShareLifecycleAndCancelCleanly(t *testing.T) {
	for _, kind := range []foundation.KernelKind{foundation.HTTP, foundation.CLI, foundation.Worker, foundation.Scheduler, foundation.WebSocket} {
		t.Run(string(kind), func(t *testing.T) {
			var closed atomic.Bool
			app := build(t, foundation.Module{Name: "kernel", OnRegister: func(r *foundation.Registrar) error {
				return r.Kernel(kind, func(*foundation.Runtime) (foundation.Kernel, error) {
					return foundation.KernelFunc(func(context.Context) error { return nil }), nil
				})
			}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
				return r.OnShutdown("resource", func(context.Context) error { closed.Store(true); return nil })
			}})
			if err := app.Run(t.Context(), kind); err != nil {
				t.Fatal(err)
			}
			if !closed.Load() || app.State() != foundation.Stopped {
				t.Fatal("kernel bypassed lifecycle")
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		entered := make(chan struct{})
		app := build(t, foundation.Module{Name: "server", OnRegister: func(r *foundation.Registrar) error {
			return r.Kernel(foundation.HTTP, func(*foundation.Runtime) (foundation.Kernel, error) {
				return foundation.KernelFunc(func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }), nil
			})
		}})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- app.Run(ctx, foundation.HTTP) }()
		await(t, entered)
		if err := app.Run(t.Context(), foundation.HTTP); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		cancel()
		if err := await(t, result); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := stop(t, app); err != nil {
			t.Fatalf("cooperative cancellation counted as failure: %v", err)
		}
	})
}

func TestKernelValidationAndMissingSelectionHaveNoSideEffects(t *testing.T) {
	app := build(t)
	if err := app.Run(t.Context(), foundation.HTTP); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	if app.State() != foundation.Prepared {
		t.Fatal("missing kernel booted application")
	}
	for _, test := range []struct {
		name    string
		factory foundation.KernelFactory
		target  fault.Code
	}{
		{"nil value", func(*foundation.Runtime) (foundation.Kernel, error) { return foundation.KernelFunc(nil), nil }, fault.Invalid},
		{"panic", func(*foundation.Runtime) (foundation.Kernel, error) { panic("secret") }, fault.Panicked},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := build(t, foundation.Module{Name: "a", OnRegister: func(r *foundation.Registrar) error { return r.Kernel(foundation.HTTP, test.factory) }})
			if err := app.Run(t.Context(), foundation.HTTP); !errors.Is(err, test.target) {
				t.Fatal(err)
			}
		})
	}
}

func TestCleanupPanicDoesNotSkipEarlierResources(t *testing.T) {
	var earlier atomic.Bool
	app := build(t, foundation.Module{Name: "a", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("first", func(context.Context) error { earlier.Store(true); return nil }); err != nil {
			return err
		}
		return r.OnShutdown("second", func(context.Context) error { panic("credential") })
	}})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := stop(t, app); !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "credential") {
		t.Fatal(err)
	}
	if !earlier.Load() {
		t.Fatal("earlier resource was skipped")
	}
}

func TestCleanupGoexitDoesNotStrandShutdown(t *testing.T) {
	var earlier atomic.Bool
	app := build(t, foundation.Module{Name: "a", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("first", func(context.Context) error { earlier.Store(true); return nil }); err != nil {
			return err
		}
		return r.OnShutdown("second", func(context.Context) error { runtime.Goexit(); return nil })
	}})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := stop(t, app); !errors.Is(err, fault.Panicked) || !earlier.Load() || app.State() != foundation.Stopped {
		t.Fatalf("cleanup Goexit stranded application: %v", err)
	}
}

func TestProviderNamesCannotCollideWithResourceOrTaskNames(t *testing.T) {
	var closed atomic.Int32
	module := func(owner foundation.ProviderID, child string) foundation.Module {
		return foundation.Module{Name: owner, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
			if err := r.OnShutdown(foundation.ResourceID(child), func(context.Context) error { closed.Add(1); return nil }); err != nil {
				return err
			}
			return r.Go(foundation.TaskID(child), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
		}}
	}
	app := build(t, module("a:b", "c"), module("a", "b:c"))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(app.PendingTasks()) != 2 {
		t.Fatal("task names collided")
	}
	if err := stop(t, app); err != nil || closed.Load() != 2 {
		t.Fatalf("resource names collided: %v", err)
	}
}

func TestBootGoexitReleasesPreviouslyAcquiredResource(t *testing.T) {
	var closed atomic.Bool
	app := build(t, foundation.Module{Name: "a", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("resource", func(context.Context) error { closed.Store(true); return nil }); err != nil {
			return err
		}
		runtime.Goexit()
		return nil
	}})
	if err := app.Start(t.Context()); !errors.Is(err, fault.Panicked) {
		t.Fatalf("boot exit not reported: %v", err)
	}
	if err := stop(t, app); !errors.Is(err, fault.Panicked) || !closed.Load() {
		t.Fatalf("boot exit leaked resource: %v", err)
	}
}

func TestCleanupDeadlineKeepsDependenciesOwnedUntilReturn(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var earlier atomic.Bool
	app, err := foundation.NewBuilder(foundation.WithShutdownTimeout(20 * time.Millisecond)).Register(foundation.Module{Name: "a", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("earlier", func(context.Context) error { earlier.Store(true); return nil }); err != nil {
			return err
		}
		return r.OnShutdown("later", func(ctx context.Context) error { close(entered); <-ctx.Done(); <-release; return nil })
	}}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Cleanup below runs after the deferred release even if an assertion fails.
	t.Cleanup(func() {
		if err := stop(t, app); !errors.Is(err, fault.Timeout) {
			t.Errorf("cleanup deadline not recorded: %v", err)
		}
		if !earlier.Load() || app.State() != foundation.Stopped {
			t.Error("cleanup did not eventually finish")
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := app.Shutdown(ctx); !errors.Is(err, fault.Timeout) {
		t.Fatalf("wait did not time out: %v", err)
	}
	await(t, entered)
	if app.State() != foundation.Stopping || earlier.Load() {
		t.Fatal("uncooperative cleanup lost ownership")
	}
}
