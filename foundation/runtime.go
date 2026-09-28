package foundation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// Runtime is a provider-scoped view of the running application. Its context is
// the application lifetime; request-specific contexts remain explicit arguments.
type Runtime struct {
	core  *runtimeState
	owner ProviderID
}
type runtimeState struct {
	ctx           context.Context
	cancel        context.CancelCauseFunc
	services      *Services
	logger        *slog.Logger
	clock         clock.Clock
	observability *observability.Recorder
	app           *App
	tasks         *taskGroup
	mu            sync.Mutex
	sealed        bool
	cleanups      []cleanup
	resourceNames map[resourceName]struct{}
}
type resourceName struct {
	owner ProviderID
	name  ResourceID
}
type taskName struct {
	owner  ProviderID
	name   TaskID
	kernel KernelKind
}

func (n taskName) String() string {
	if n.kernel != "" {
		return "kernel:" + string(n.kernel)
	}
	return fmt.Sprintf("provider[%s]:%s", n.owner, n.name)
}

type cleanup struct {
	name        string
	close       func(context.Context) error
	observation observability.Name
}

func newRuntime(ctx context.Context, services *Services, logger *slog.Logger, source clock.Clock, recorder *observability.Recorder, app *App) *runtimeState {
	ctx = observability.WithContext(ctx, recorder)
	lifetime, cancel := context.WithCancelCause(ctx)
	return &runtimeState{ctx: lifetime, cancel: cancel, services: services, logger: logger, clock: source, observability: recorder, app: app,
		tasks: newTaskGroup(lifetime, cancel), resourceNames: make(map[resourceName]struct{})}
}

func (r *Runtime) Context() context.Context { return r.core.ctx }
func (r *Runtime) Services() *Services      { return r.core.services }
func (r *Runtime) Logger() *slog.Logger     { return r.core.logger.With("provider", string(r.owner)) }
func (r *Runtime) Owner() ProviderID        { return r.owner }
func (r *Runtime) Clock() clock.Clock       { return r.core.clock }

// State reports the owning application's synchronized lifecycle state. It
// does not infer readiness from individual dependency availability.
func (r *Runtime) State() State { return r.core.app.State() }

// Go starts a tracked critical task. Any non-cancellation error initiates
// application shutdown. Use jobs for durable work, not unmanaged goroutines.
func (r *Runtime) Go(name TaskID, run func(context.Context) error) error {
	if !validName(string(name)) {
		return fault.New(fault.Invalid, "invalid task ID")
	}
	if run == nil {
		return fault.New(fault.Invalid, "managed task requires a callback")
	}
	return r.core.tasks.start(taskName{owner: r.owner, name: name}, func(ctx context.Context) error {
		return r.core.observe(ctx, observability.Operation{Kind: observability.Resource, Name: observationName(string(r.owner) + "." + string(name))}, run)
	})
}

// OnShutdown records an acquired resource immediately, including during partial
// startup. Cleanups run once in reverse acquisition order, after all managed
// work exits. Callers retain ownership if registration returns an error.
func (r *Runtime) OnShutdown(name ResourceID, close func(context.Context) error) error {
	if !validName(string(name)) || close == nil {
		return fault.New(fault.Invalid, "invalid resource cleanup")
	}
	key := resourceName{r.owner, name}
	label := fmt.Sprintf("provider[%s]:%s", r.owner, name)
	r.core.mu.Lock()
	defer r.core.mu.Unlock()
	if r.core.sealed {
		return fault.New(fault.Closed, "resource registration is closed")
	}
	if _, exists := r.core.resourceNames[key]; exists {
		return fault.New(fault.Duplicate, "resource "+label+" is already registered")
	}
	r.core.resourceNames[key] = struct{}{}
	r.core.cleanups = append(r.core.cleanups, cleanup{label, close, observationName(string(r.owner) + "." + string(name))})
	return nil
}

func (r *Runtime) onPluginShutdown(close func(context.Context) error) error {
	r.core.mu.Lock()
	defer r.core.mu.Unlock()
	if r.core.sealed {
		return fault.New(fault.Closed, "plugin shutdown registration is closed")
	}
	r.core.cleanups = append(r.core.cleanups, cleanup{"plugin shutdown " + string(r.owner), close, observationName(string(r.owner) + ".shutdown")})
	return nil
}

type taskGroup struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelCauseFunc
	active   map[taskName]struct{}
	sealed   bool
	done     chan struct{}
	failures []error
}

func newTaskGroup(ctx context.Context, cancel context.CancelCauseFunc) *taskGroup {
	return &taskGroup{ctx: ctx, cancel: cancel, active: make(map[taskName]struct{}), done: make(chan struct{})}
}
func (g *taskGroup) start(name taskName, run func(context.Context) error) error {
	if run == nil {
		return fault.New(fault.Invalid, "nil managed task")
	}
	g.mu.Lock()
	if g.sealed || g.ctx.Err() != nil {
		g.mu.Unlock()
		return fault.New(fault.Closed, "managed tasks are stopping")
	}
	if _, exists := g.active[name]; exists {
		g.mu.Unlock()
		return fault.New(fault.Duplicate, "task "+name.String()+" is already active")
	}
	g.active[name] = struct{}{}
	g.mu.Unlock()
	go func() {
		var err error = fault.New(fault.Panicked, "task "+name.String()+" exited without returning")
		defer func() { g.finish(name, err) }()
		err = invoke("task "+name.String(), func() error {
			err := run(g.ctx)
			if g.ctx.Err() != nil && cancellationOnly(err) {
				return nil
			}
			return err
		})
	}()
	return nil
}

func (g *taskGroup) finish(name taskName, err error) {
	g.mu.Lock()
	if err != nil {
		g.failures = append(g.failures, err)
		g.cancel(err)
	}
	delete(g.active, name)
	if g.sealed && len(g.active) == 0 {
		close(g.done)
	}
	g.mu.Unlock()
}
func (g *taskGroup) seal() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.sealed {
		g.sealed = true
		if len(g.active) == 0 {
			close(g.done)
		}
	}
	return g.done
}
func (g *taskGroup) pending() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	names := make([]string, 0, len(g.active))
	for name := range g.active {
		names = append(names, name.String())
	}
	sort.Strings(names)
	return names
}
func (g *taskGroup) err() error { g.mu.Lock(); defer g.mu.Unlock(); return errors.Join(g.failures...) }

// Only suppress an error when every leaf is ordinary cancellation. Joining a
// real failure with cancellation must retain that failure. Bound traversal so a
// cyclic or excessively deep error graph cannot strand normal shutdown.
func cancellationOnly(err error) bool {
	if err == nil {
		return false
	}
	cancelled := false
	inspected := callback.Isolated("classify lifecycle cancellation", func() error {
		remaining := 256
		var visit func(error, int) bool
		visit = func(err error, depth int) bool {
			remaining--
			if err == nil || depth > 64 || remaining < 0 {
				return false
			}
			if err == context.Canceled || err == context.DeadlineExceeded {
				return true
			}
			// A classified framework failure (for example an expired HTTP
			// shutdown grace) remains a failure even when its cause is a
			// context deadline. Plain wrapping/joining adds no classification.
			if _, classified := err.(*fault.Error); classified {
				return false
			}
			switch e := err.(type) {
			case interface{ Unwrap() []error }:
				children := e.Unwrap()
				if len(children) == 0 || len(children) > remaining {
					return false
				}
				seen := false
				for _, child := range children {
					if child == nil {
						continue
					}
					seen = true
					if !visit(child, depth+1) {
						return false
					}
				}
				return seen
			case interface{ Unwrap() error }:
				return visit(e.Unwrap(), depth+1)
			default:
				return false
			}
		}
		cancelled = visit(err, 0)
		return nil
	})
	return inspected == nil && cancelled
}
