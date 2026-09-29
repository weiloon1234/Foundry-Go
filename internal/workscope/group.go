// Package workscope owns bounded operation lifetimes for borrowed-resource services.
package workscope

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// Group has bounded, queued admission and no background goroutine or owned
// dependency. A burst waits in FIFO order for at most admission.Wait(timeout)
// (and the caller's deadline); an unsatisfied wait is fault.Overloaded. Nested
// admission into the same group never waits, so an operation cannot deadlock
// on capacity held by its own callers. Run retains its slot until the isolated
// callback actually exits, even when the caller or Close cancels. Never copy a
// Group. Construct it with New.
type Group struct {
	mu      sync.Mutex
	active  int
	slots   *admission.Semaphore
	timeout time.Duration
	closing bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
}

func New(maximum int, timeout time.Duration) (*Group, error) {
	if maximum < 1 || maximum > 4096 || timeout <= 0 || timeout > 24*time.Hour {
		return nil, fault.New(fault.Invalid, "invalid operation scope")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Group{slots: admission.New(maximum), timeout: timeout, ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
}

type frameKey struct{}
type frame struct {
	group  *Group
	parent *frame
	active atomic.Bool
}

// Lease retains admission until its owner has actually released every resource.
// Cancellation ends its context, not ownership. Never copy a Lease. Release is
// idempotent and must follow the last callback/read using the borrowed resource.
type Lease struct {
	ctx     context.Context
	release func()
	once    sync.Once
}

func (l *Lease) Context() context.Context { return l.ctx }
func (l *Lease) Release() {
	if l != nil && l.release != nil {
		l.once.Do(l.release)
	}
}

// Begin admits a resource whose lifetime can extend beyond one function call.
// Run uses the same admission path for callback-scoped operations.
func (g *Group) Begin(ctx context.Context) (*Lease, error) {
	if g == nil || g.done == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "invalid operation scope")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	closing := g.closing
	g.mu.Unlock()
	if closing {
		return nil, fault.New(fault.Closed, "operation scope is closed")
	}
	if g.nested(ctx) {
		if !g.slots.TryAcquire() {
			return nil, fault.New(fault.Overloaded, "service has no available operation capacity")
		}
	} else if err := g.slots.Acquire(ctx, admission.Wait(g.timeout), g.ctx.Done()); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.closing {
		g.mu.Unlock()
		g.slots.Release()
		return nil, fault.New(fault.Closed, "operation scope is closed")
	}
	g.active++
	g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	// Keep the link outermost so Err synchronously observes owner shutdown,
	// even before its cancellation callback has run.
	ctx, unlink := contextlink.Link(ctx, g.ctx)
	parent, _ := ctx.Value(frameKey{}).(*frame)
	f := &frame{group: g, parent: parent}
	f.active.Store(true)
	ctx = context.WithValue(ctx, frameKey{}, f)
	return &Lease{ctx: ctx, release: func() {
		f.active.Store(false)
		cancel()
		unlink()
		g.mu.Lock()
		defer g.mu.Unlock()
		g.active--
		g.slots.Release()
		if g.closing && g.active == 0 {
			close(g.done)
		}
	}}, nil
}

// nested reports whether ctx already holds an active admission of this group.
func (g *Group) nested(ctx context.Context) bool {
	for f, _ := ctx.Value(frameKey{}).(*frame); f != nil; f = f.parent {
		if f.group == g && f.active.Load() {
			return true
		}
	}
	return false
}

func (g *Group) Run(ctx context.Context, name string, fn func(context.Context) error) error {
	if fn == nil {
		return fault.New(fault.Invalid, "operation scope requires a callback")
	}
	lease, err := g.Begin(ctx)
	if err != nil {
		return err
	}
	defer lease.Release()
	ctx = lease.Context()
	return callback.Isolated(name, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(ctx)
	})
}
func (g *Group) Done() <-chan struct{} {
	if g == nil || g.done == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return g.done
}

// CheckClose checks the caller's ownership before a service cancels several
// groups together. It does not change admission or wait for active work.
func (g *Group) CheckClose(ctx context.Context) error {
	if g == nil || g.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "invalid operation scope")
	}
	for f, _ := ctx.Value(frameKey{}).(*frame); f != nil; f = f.parent {
		if f.group == g && f.active.Load() {
			return fault.New(fault.Cycle, "operation cannot wait for its own exit")
		}
	}
	return nil
}
func (g *Group) Close(ctx context.Context) error {
	if err := g.CheckClose(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	if !g.closing {
		g.closing = true
		g.cancel()
		if g.active == 0 {
			close(g.done)
		}
	}
	g.mu.Unlock()
	select {
	case <-g.done:
		return nil
	default:
	}
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
