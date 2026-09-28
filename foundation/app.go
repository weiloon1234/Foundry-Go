package foundation

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// State reports actual lifecycle state. A timed-out Shutdown wait leaves an
// application Stopping until its outstanding work and cleanup really finish.
type State string

const (
	Prepared State = "prepared"
	Starting State = "starting"
	Running  State = "running"
	Stopping State = "stopping"
	Stopped  State = "stopped"
)

// App owns provider startup, a selected kernel and reverse-order shutdown.
// Services are available after Build; external resources become ready at Start.
type App struct {
	mu          sync.Mutex
	settings    settings
	providers   []providerEntry
	services    *Services
	inspection  Inspection
	kernels     map[KernelKind]kernelDefinition
	state       State
	runtime     *runtimeState
	startDone   chan struct{}
	startErr    error
	done        chan struct{}
	shutdownErr error
	stopOnce    sync.Once
	runClaimed  bool
}

func newApp(settings settings, providers []providerEntry, services *Services, kernels map[KernelKind]kernelDefinition) *App {
	return &App{settings: settings, providers: providers, services: services, kernels: kernels,
		state: Prepared, startDone: make(chan struct{}), done: make(chan struct{})}
}

func (a *App) Services() *Services            { return a.services }
func (a *App) Inspect() Inspection            { return a.inspection.snapshot() }
func (a *App) Logger() *slog.Logger           { return a.settings.logger }
func (a *App) ShutdownTimeout() time.Duration { return a.settings.shutdownTimeout }
func (a *App) State() State                   { a.mu.Lock(); defer a.mu.Unlock(); return a.state }
func (a *App) Done() <-chan struct{}          { return a.done }

// PendingTasks reports currently active, provider-qualified task names.
func (a *App) PendingTasks() []string {
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.tasks.pending()
}

// Start boots the application once. The first caller's context owns its lifetime.
// Concurrent callers wait for the same startup result using their own deadline.
func (a *App) Start(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "nil start context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	switch a.state {
	case Prepared:
		a.state = Starting
		a.runtime = newRuntime(ctx, a.services, a.settings.logger, a.settings.clock, a.settings.observability, a)
		go a.boot()
		context.AfterFunc(a.runtime.ctx, a.beginShutdown)
	case Stopping, Stopped:
		a.mu.Unlock()
		return fault.New(fault.Closed, "application is stopping or stopped")
	}
	a.mu.Unlock()
	select {
	case <-a.startDone:
		a.mu.Lock()
		err := a.startErr
		a.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) boot() {
	var err error = fault.New(fault.Panicked, "provider boot exited without returning")
	defer func() { a.completeBoot(err) }()
	err = a.bootProviders()
}

func (a *App) bootProviders() error {
	if err := a.runtime.startObservability(); err != nil {
		return err
	}
	for _, entry := range a.providers {
		if err := context.Cause(a.runtime.ctx); err != nil {
			return err
		}
		if booter, ok := entry.provider.(Booter); ok {
			err := a.runtime.observe(a.runtime.ctx, observability.Operation{Kind: observability.Provider, Name: observationName(string(entry.id))}, func(ctx context.Context) error {
				return invoke("boot provider "+string(entry.id), func() error { return booter.Boot(ctx, &Runtime{a.runtime, entry.id}) })
			})
			if err != nil {
				return err
			}
		}
	}
	return context.Cause(a.runtime.ctx)
}

func (a *App) completeBoot(err error) {
	a.mu.Lock()
	if err == nil && a.state == Stopping {
		err = context.Canceled
	}
	a.startErr = err
	if err == nil && a.state == Starting {
		a.state = Running
	}
	close(a.startDone)
	a.mu.Unlock()
	if err != nil {
		a.runtime.cancel(err)
		a.beginShutdown()
	}
}

// Run starts providers and executes one registered runtime kernel. Completion,
// cancellation or a critical task failure initiates shutdown automatically.
func (a *App) Run(ctx context.Context, kind KernelKind) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "nil run context")
	}
	a.mu.Lock()
	definition, exists := a.kernels[kind]
	if !exists {
		a.mu.Unlock()
		return fault.New(fault.Missing, "kernel "+string(kind)+" is not registered")
	}
	if a.runClaimed {
		a.mu.Unlock()
		return fault.New(fault.Conflict, "application kernel has already been selected")
	}
	a.runClaimed = true
	a.mu.Unlock()
	if err := a.Start(ctx); err != nil {
		return a.finishRun(err)
	}
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	result := make(chan error, 1)
	err := runtime.tasks.start(taskName{kernel: kind}, func(lifetime context.Context) error {
		runCtx, cancel := context.WithCancel(lifetime)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		var kernel Kernel
		err := runtime.observe(runCtx, observability.Operation{Kind: observability.Kernel, Name: observationName(string(kind) + ".construct")}, func(context.Context) error {
			return invoke("construct kernel "+string(kind), func() error {
				var err error
				kernel, err = definition.factory(&Runtime{runtime, definition.owner})
				if err == nil && nilValue(kernel) {
					return fault.New(fault.Invalid, "kernel factory returned nil")
				}
				return err
			})
		})
		if err == nil {
			err = runtime.observe(runCtx, observability.Operation{Kind: observability.Kernel, Name: observability.Name(kind)}, func(ctx context.Context) error {
				return invoke("run kernel "+string(kind), func() error { return kernel.Run(ctx) })
			})
		}
		result <- err
		if runCtx.Err() != nil && cancellationOnly(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return a.finishRun(err)
	}
	select {
	case err = <-result:
	case <-ctx.Done():
		err = ctx.Err()
	case <-runtime.ctx.Done():
		err = context.Cause(runtime.ctx)
	}
	if err == nil {
		err = context.Cause(runtime.ctx)
	}
	if err == nil {
		err = ctx.Err()
	}
	return a.finishRun(err)
}

func (a *App) finishRun(err error) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.settings.shutdownTimeout)
	defer cancel()
	stopErr := a.Shutdown(ctx)
	return errors.Join(err, stopErr)
}

// Shutdown requests cancellation and waits up to the caller's deadline. A
// timeout does not kill Go goroutines or close their dependencies underneath
// them. Cleanup continues, and callers can wait again on Shutdown or Done.
func (a *App) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "nil shutdown context")
	}
	a.beginShutdown()
	select {
	case <-a.done:
		a.mu.Lock()
		err := a.shutdownErr
		a.mu.Unlock()
		return err
	default:
	}
	select {
	case <-a.done:
		a.mu.Lock()
		err := a.shutdownErr
		a.mu.Unlock()
		return err
	case <-ctx.Done():
		return fault.Wrap(fault.Timeout, "application shutdown is still pending", ctx.Err())
	}
}

func (a *App) beginShutdown() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		if a.state == Prepared {
			a.state = Stopped
			a.startErr = fault.New(fault.Closed, "application was stopped before startup")
			close(a.startDone)
			if recorder := a.settings.observability; recorder != nil {
				a.state = Stopping
				timeout := a.settings.shutdownTimeout
				a.mu.Unlock()
				go a.stopPreparedObservability(recorder, timeout)
				return
			}
			close(a.done)
			a.mu.Unlock()
			return
		}
		a.state = Stopping
		runtime := a.runtime
		a.mu.Unlock()
		runtime.observability.Gate().Drain()
		runtime.cancel(context.Canceled)
		go a.stop()
	})
}

func (a *App) stop() {
	<-a.startDone
	runtime := a.runtime
	<-runtime.tasks.seal()
	failures := []error{runtime.tasks.err()}
	a.mu.Lock()
	startErr := a.startErr
	a.mu.Unlock()
	// Inspect callback errors outside the application mutex.
	if startErr != nil && !cancellationOnly(startErr) {
		failures = append(failures, startErr)
	}
	runtime.mu.Lock()
	runtime.sealed = true
	cleanups := append([]cleanup(nil), runtime.cleanups...)
	runtime.mu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		resource := cleanups[i]
		ctx, cancel := context.WithTimeout(context.Background(), a.settings.shutdownTimeout)
		var err error
		if resource.observation != "" {
			err = runtime.observe(ctx, observability.Operation{Kind: observability.Resource, Name: resource.observation}, func(ctx context.Context) error { return closeResource(ctx, resource) })
		} else {
			err = closeResource(ctx, resource)
		}
		if err == nil && ctx.Err() != nil {
			err = fault.Wrap(fault.Timeout, "cleanup exceeded its deadline: "+resource.name, ctx.Err())
		}
		cancel()
		if err != nil {
			failures = append(failures, err)
		}
	}
	a.mu.Lock()
	a.shutdownErr = errors.Join(failures...)
	a.state = Stopped
	close(a.done)
	a.mu.Unlock()
}

// Isolate cleanup Goexit as well as panic. The coordinator still waits for the
// callback to finish, so it never closes an earlier dependency prematurely.
func closeResource(ctx context.Context, resource cleanup) error {
	return callback.Isolated("close resource "+resource.name, func() error { return resource.close(ctx) })
}
