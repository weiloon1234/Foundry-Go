package foundation

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/maintenance"
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

// stopReason distinguishes an owner's request, which honors the configured
// stop delay, from kernel completion or failure, which stop immediately.
type stopReason string

const (
	stopRequested stopReason = "requested"
	stopFailed    stopReason = "failure"
)

// App owns provider startup, a selected kernel and reverse-order shutdown.
// Services are available after Build; external resources become ready at Start.
type App struct {
	mu         sync.Mutex
	settings   settings
	providers  []providerEntry
	services   *Services
	inspection Inspection
	kernels    map[KernelKind]kernelDefinition
	state      State
	runtime    *runtimeState
	startDone  chan struct{}
	startErr   error
	bootErr    error
	// startupExpired is the cause the startup deadline cancelled boot with.
	startupExpired error
	done           chan struct{}
	cleanupErr     error
	shutdownErr    error
	stopOnce       sync.Once
	runClaimed     bool
	selected       []KernelKind
	started        time.Time
	stopStarted    time.Time
	deadline       time.Time
}

func newApp(settings settings, providers []providerEntry, services *Services, kernels map[KernelKind]kernelDefinition) *App {
	return &App{settings: settings, providers: providers, services: services, kernels: kernels,
		state: Prepared, startDone: make(chan struct{}), done: make(chan struct{})}
}

func (a *App) Services() *Services            { return a.services }
func (a *App) Inspect() Inspection            { return a.inspection.snapshot() }
func (a *App) Logger() *slog.Logger           { return a.settings.logger }
func (a *App) ShutdownTimeout() time.Duration { return a.settings.shutdownTimeout }
func (a *App) StopDelay() time.Duration       { return a.settings.stopDelay }
func (a *App) State() State                   { a.mu.Lock(); defer a.mu.Unlock(); return a.state }
func (a *App) Done() <-chan struct{}          { return a.done }

// selectedKernels returns an owned copy of the kernels chosen by Run/RunKernels.
func (a *App) selectedKernels() []KernelKind {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.selected)
}

// Maintenance returns the application's admission gate. It exists whether or
// not observability is configured and is carried by every runtime context.
func (a *App) Maintenance() *maintenance.Gate { return a.settings.maintenance }

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

// Start boots the application once. The first caller's context owns its lifetime:
// its cancellation requests shutdown, including the configured stop delay.
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
		a.started = time.Now()
		// Owner cancellation is a shutdown request, not an immediate kernel
		// cancellation: the stop delay keeps serving while readiness fails.
		a.runtime = newRuntime(context.WithoutCancel(ctx), a.services, a.settings.logger, a.settings.clock, a.settings.observability, a.settings.maintenance, a)
		a.settings.logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelInfo, "application starting", slog.Int("providers", len(a.providers)))
		go a.boot()
		context.AfterFunc(ctx, func() { a.beginShutdown(stopRequested) })
		context.AfterFunc(a.runtime.ctx, func() { a.beginShutdown(stopFailed) })
		if timeout := a.settings.startupTimeout; timeout > 0 {
			runtime := a.runtime
			expired := fault.New(fault.Timeout, "application startup exceeded its deadline")
			timer := time.AfterFunc(timeout, func() {
				a.mu.Lock()
				starting := a.state == Starting
				if starting {
					a.startupExpired = expired
				}
				a.mu.Unlock()
				if starting {
					runtime.cancel(expired)
				}
			})
			go func() { <-a.startDone; timer.Stop() }()
		}
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
	// Providers usually return ctx.Err() when the startup deadline cancels
	// them; report the deadline itself, not an interrupt. Error classification
	// runs outside the application mutex.
	if err != nil && cancellationOnly(err) {
		a.mu.Lock()
		expired := a.startupExpired
		a.mu.Unlock()
		if expired != nil && context.Cause(a.runtime.ctx) == expired {
			err = expired
		}
	}
	a.mu.Lock()
	elapsed, stopping := time.Since(a.started), a.state == Stopping
	a.mu.Unlock()
	// Log before releasing Start's waiters (and so the shutdown that follows
	// a failure), so the record precedes anything a caller observes next.
	if err != nil && !cancellationOnly(err) {
		a.settings.logger.LogAttrs(context.Background(), slog.LevelError, "application startup failed", slog.Duration("duration", elapsed), slog.Any("diagnostic", errordiag.Describe(err)))
	} else if err == nil && !stopping {
		a.settings.logger.LogAttrs(context.Background(), slog.LevelInfo, "application ready", slog.Duration("duration", elapsed))
	}
	a.mu.Lock()
	if err == nil && a.state == Stopping {
		err = context.Canceled
	}
	a.startErr = err
	a.bootErr = err
	if err == nil && a.state == Starting {
		a.state = Running
	}
	close(a.startDone)
	a.mu.Unlock()
	if err != nil {
		a.runtime.cancel(err)
		a.beginShutdown(stopFailed)
	}
}

// Run starts providers and executes one registered runtime kernel. Completion,
// cancellation or a critical task failure initiates shutdown automatically.
// Cancelling ctx is a graceful stop: a service kernel (HTTP, worker, scheduler,
// WebSocket) that drains cooperatively and whose shutdown completes cleanly
// returns nil. A CLI kernel is one-shot, so its interruption stays an error.
// Each failure is reported once. Run waits for shutdown up to the application
// shutdown budget, which covers the stop delay, kernel drain and every cleanup.
func (a *App) Run(ctx context.Context, kind KernelKind) error {
	return a.RunKernels(ctx, kind)
}

// RunKernels runs several service kernels in one process, for example HTTP and
// a worker, sharing one provider lifetime. The first kernel to finish, a caller
// cancellation or a critical task failure stops every kernel. The CLI kernel is
// one-shot and must run alone. Kinds must be distinct and registered.
func (a *App) RunKernels(ctx context.Context, kinds ...KernelKind) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "nil run context")
	}
	if len(kinds) == 0 {
		return fault.New(fault.Invalid, "run requires at least one kernel")
	}
	a.mu.Lock()
	definitions := make([]kernelDefinition, len(kinds))
	for i, kind := range kinds {
		definition, exists := a.kernels[kind]
		if !exists {
			a.mu.Unlock()
			return fault.New(fault.Missing, "kernel "+string(kind)+" is not registered")
		}
		if slices.Index(kinds, kind) != i {
			a.mu.Unlock()
			return fault.New(fault.Duplicate, "kernel "+string(kind)+" is selected more than once")
		}
		if kind == CLI && len(kinds) > 1 {
			a.mu.Unlock()
			return fault.New(fault.Invalid, "the CLI kernel must run alone")
		}
		definitions[i] = definition
	}
	if a.runClaimed {
		a.mu.Unlock()
		return fault.New(fault.Conflict, "application kernel has already been selected")
	}
	a.runClaimed = true
	a.selected = slices.Clone(kinds)
	a.mu.Unlock()
	if err := a.Start(ctx); err != nil {
		// Start reported the boot failure; shutdown reports only cleanup.
		return errors.Join(err, a.awaitStop(stopRequested))
	}
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	results := make(chan error, len(kinds))
	launched := 0
	var launchErr error
	for i, kind := range kinds {
		definition := definitions[i]
		// Kernel results travel to Run, which reports them exactly once. The task
		// group still cancels the shared lifetime when a kernel fails.
		err := runtime.tasks.startReported(taskName{kernel: kind}, func(lifetime context.Context) error {
			err := runtime.runKernel(lifetime, ctx, kind, definition)
			results <- err
			return err
		})
		if err != nil {
			launchErr = err
			break
		}
		launched++
	}
	cancelled := false
	var failures []error
	if launchErr != nil {
		failures = append(failures, launchErr)
	} else {
		select {
		case err := <-results:
			launched--
			if err != nil {
				failures = append(failures, err)
			}
		case <-ctx.Done():
			cancelled = true
		case <-runtime.ctx.Done():
		}
	}
	reason := stopFailed
	if cancelled {
		reason = stopRequested
	}
	stopErr := a.awaitStop(reason)
	// Kernels still running when shutdown began have exited if it completed.
collect:
	for ; launched > 0; launched-- {
		select {
		case err := <-results:
			if err != nil {
				failures = append(failures, err)
			}
		default:
			break collect
		}
	}
	return errors.Join(append(failures, stopErr)...)
}

func (r *runtimeState) runKernel(lifetime, caller context.Context, kind KernelKind, definition kernelDefinition) error {
	runCtx, cancel := context.WithCancel(lifetime)
	defer cancel()
	// A CLI kernel stops as soon as its caller is interrupted. Service kernels
	// stop when the application lifetime ends, after any configured stop delay.
	if kind == CLI {
		stop := context.AfterFunc(caller, cancel)
		defer stop()
	}
	var kernel Kernel
	err := r.observe(runCtx, observability.Operation{Kind: observability.Kernel, Name: observationName(string(kind) + ".construct")}, func(context.Context) error {
		return invoke("construct kernel "+string(kind), func() error {
			var err error
			kernel, err = definition.factory(&Runtime{r, definition.owner})
			if err == nil && nilValue(kernel) {
				return fault.New(fault.Invalid, "kernel factory returned nil")
			}
			return err
		})
	})
	if err == nil {
		err = r.observe(runCtx, observability.Operation{Kind: observability.Kernel, Name: observability.Name(kind)}, func(ctx context.Context) error {
			return invoke("run kernel "+string(kind), func() error { return kernel.Run(ctx) })
		})
	}
	// A drained service kernel stopped gracefully. A one-shot command that was
	// interrupted did not complete, so its cancellation remains its result.
	if kind != CLI && runCtx.Err() != nil && cancellationOnly(err) {
		return nil
	}
	return err
}

// awaitStop begins shutdown and waits until the whole application budget has
// elapsed. It returns task and cleanup failures only; startup failures belong
// to the Start result.
func (a *App) awaitStop(reason stopReason) error {
	a.beginShutdown(reason)
	a.mu.Lock()
	deadline := a.deadline
	a.mu.Unlock()
	select {
	case <-a.done:
	default:
		wait := time.Until(deadline) + stopMargin(a.settings.shutdownTimeout)
		if deadline.IsZero() {
			wait = a.settings.shutdownTimeout
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-a.done:
		case <-timer.C:
			return fault.Wrap(fault.Timeout, "application shutdown is still pending", context.DeadlineExceeded)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cleanupErr
}

// stopMargin lets cooperative cleanups observe the shared deadline and return
// before Run reports a pending shutdown.
func stopMargin(budget time.Duration) time.Duration { return min(time.Second, budget/10) }

// Shutdown requests cancellation and waits up to the caller's deadline. A
// timeout does not kill Go goroutines or close their dependencies underneath
// them. Cleanup continues, and callers can wait again on Shutdown or Done.
// The configured stop delay applies to the first request on a running app.
func (a *App) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "nil shutdown context")
	}
	a.beginShutdown(stopRequested)
	select {
	case <-a.done:
		return a.shutdownResult()
	default:
	}
	select {
	case <-a.done:
		return a.shutdownResult()
	case <-ctx.Done():
		return fault.Wrap(fault.Timeout, "application shutdown is still pending", ctx.Err())
	}
}

// shutdownResult is computed once so repeated calls return the same error.
func (a *App) shutdownResult() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.shutdownErr
}

func (a *App) beginShutdown(reason stopReason) {
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
		delay := time.Duration(0)
		if reason == stopRequested && a.state == Running {
			delay = a.settings.stopDelay
		}
		a.state = Stopping
		a.stopStarted = time.Now()
		a.deadline = a.stopStarted.Add(a.settings.shutdownTimeout)
		runtime := a.runtime
		a.mu.Unlock()
		a.settings.logger.LogAttrs(context.Background(), slog.LevelInfo, "application shutdown started", slog.String("reason", string(reason)), slog.Duration("stop_delay", delay), slog.Duration("budget", a.settings.shutdownTimeout))
		if delay == 0 {
			a.settings.maintenance.Drain()
			runtime.cancel(context.Canceled)
		}
		go a.stop(delay)
	})
}

func (a *App) stop(delay time.Duration) {
	runtime := a.runtime
	if delay > 0 {
		// Lame duck: readiness reports Stopping while kernels keep serving so load
		// balancers can deregister this instance before admission closes. A
		// failure during the delay stops immediately.
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-runtime.ctx.Done():
			timer.Stop()
		}
		a.settings.maintenance.Drain()
		runtime.cancel(context.Canceled)
	}
	<-a.startDone
	<-runtime.tasks.seal()
	failures := []error{runtime.tasks.err()}
	runtime.mu.Lock()
	runtime.sealed = true
	cleanups := append([]cleanup(nil), runtime.cleanups...)
	runtime.mu.Unlock()
	a.mu.Lock()
	deadline := a.deadline
	a.mu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		resource := cleanups[i]
		// Cleanups share what remains of one budget that began with shutdown.
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		var err error
		if resource.observation != "" {
			err = runtime.observe(ctx, observability.Operation{Kind: observability.Resource, Name: resource.observation}, func(ctx context.Context) error { return closeResource(ctx, resource) })
		} else {
			err = closeResource(ctx, resource)
		}
		timedOut := err == nil && ctx.Err() != nil
		if timedOut {
			err = fault.Wrap(fault.Timeout, "cleanup exceeded its deadline: "+resource.name, ctx.Err())
		}
		cancel()
		if err != nil {
			message := "application cleanup failed"
			if timedOut {
				message = "application cleanup timed out"
			}
			a.settings.logger.LogAttrs(context.Background(), slog.LevelError, message, slog.String("resource", resource.name), slog.Any("diagnostic", errordiag.Describe(err)))
			failures = append(failures, err)
		}
	}
	result := errors.Join(failures...)
	a.mu.Lock()
	bootErr := a.bootErr
	elapsed := time.Since(a.stopStarted)
	a.mu.Unlock()
	// Shutdown also reports a boot failure; Run reported it through Start.
	// Inspect callback errors outside the application mutex.
	shutdownErr := result
	if bootErr != nil && !cancellationOnly(bootErr) {
		shutdownErr = errors.Join(bootErr, result)
	}
	// Log before releasing waiters: a process that exits as soon as Run or
	// Shutdown returns must not lose its final lifecycle record.
	if result != nil {
		a.settings.logger.LogAttrs(context.Background(), slog.LevelError, "application stopped with failures", slog.Duration("duration", elapsed), slog.Any("diagnostic", errordiag.Describe(result)))
	} else {
		a.settings.logger.LogAttrs(context.Background(), slog.LevelInfo, "application stopped", slog.Duration("duration", elapsed))
	}
	a.mu.Lock()
	a.cleanupErr = result
	a.shutdownErr = shutdownErr
	a.state = Stopped
	close(a.done)
	a.mu.Unlock()
}

// Isolate cleanup Goexit as well as panic. The coordinator still waits for the
// callback to finish, so it never closes an earlier dependency prematurely.
func closeResource(ctx context.Context, resource cleanup) error {
	return callback.Isolated("close resource "+resource.name, func() error { return resource.close(ctx) })
}
