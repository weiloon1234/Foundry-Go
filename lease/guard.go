package lease

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// Guard represents one ownership interval. It has no finalizer: release it
// explicitly, cancel its parent, or close its manager. Expiry also owns cleanup.
// Context cancellation is cooperative; external writes require resource fencing
// when stale-process exclusion is necessary. Guards cannot be copied usefully.
type Guard struct {
	manager    *Manager
	key        Key
	owner      Owner
	ttl        time.Duration
	ctx        context.Context
	parent     context.Context
	cancel     context.CancelCauseFunc
	unlink     context.CancelFunc
	mu         sync.Mutex
	until      time.Time
	timer      *time.Timer
	gate       chan struct{}
	done       chan struct{}
	cleanupErr error
	// exported ends local management without releasing the authority key,
	// which another process now owns through a restored Token.
	exported atomic.Bool
}

func newGuard(m *Manager, parent context.Context, unlink context.CancelFunc, key Key, owner Owner, ttl time.Duration, until time.Time, heartbeat bool, finish func()) *Guard {
	ctx, cancel := context.WithCancelCause(parent)
	g := &Guard{manager: m, key: key, owner: owner, ttl: ttl, ctx: ctx, parent: parent, cancel: cancel, unlink: unlink, until: until, gate: make(chan struct{}, 1), done: make(chan struct{})}
	g.gate <- struct{}{}
	g.timer = time.AfterFunc(time.Until(until), g.expire)
	go g.run(heartbeat, finish)
	return g
}
func (g *Guard) expire() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !time.Now().Before(g.until) {
		g.cancel(ErrLost)
	}
}

// Context preserves caller values and is canceled on release, expiry, renewal
// failure, caller cancellation or manager shutdown. A zero guard returns nil.
func (g *Guard) Context() context.Context {
	if g == nil {
		return nil
	}
	return g.ctx
}

// Err rechecks validity synchronously, including after a process scheduling pause.
func (g *Guard) Err() error {
	if g == nil || g.ctx == nil {
		return fault.New(fault.Invalid, "lease guard is not initialized")
	}
	g.expire()
	if err := g.parent.Err(); err != nil {
		g.cancel(err)
	}
	return context.Cause(g.ctx)
}

// Renew compares the opaque owner atomically. It cannot revive a locally expired
// interval, even if the authority accepted a late response. Only one renewal per
// guard can run; concurrent attempts fail with Conflict instead of queuing.
func (g *Guard) Renew(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "lease renewal requires a context")
	}
	if err := g.Err(); err != nil {
		return err
	}
	select {
	case <-g.gate:
		defer func() { g.gate <- struct{}{} }()
	default:
		return fault.New(fault.Conflict, "lease renewal is already running")
	}
	if err := g.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	previous := g.until
	g.mu.Unlock()
	linked, unlink := contextlink.Link(ctx, g.ctx)
	defer unlink()
	deadline, cancel := context.WithDeadline(linked, previous)
	defer cancel()
	next := time.Now().Add(validity(g.ttl))
	ok, err := g.manager.command(deadline, func(ctx context.Context) (bool, error) {
		return g.manager.backend.LeaseRenew(ctx, g.key, g.owner, g.ttl)
	})
	g.mu.Lock()
	defer g.mu.Unlock()
	if err != nil || !ok || g.ctx.Err() != nil || !time.Now().Before(previous) || !time.Now().Before(next) {
		cause := errors.Join(ErrLost, err, context.Cause(g.ctx))
		g.cancel(cause)
		return cause
	}
	g.until = next
	g.timer.Reset(time.Until(next))
	return nil
}
func (g *Guard) run(heartbeat bool, finish func()) {
	var ticks <-chan time.Time
	var ticker *time.Ticker
	if heartbeat {
		ticker = time.NewTicker(g.ttl / 3)
		ticks = ticker.C
	}
	running := true
	for running {
		select {
		case <-g.ctx.Done():
			running = false
		case <-ticks:
			if err := g.Renew(g.ctx); err != nil {
				g.cancel(err)
			}
		}
	}
	if ticker != nil {
		ticker.Stop()
	}
	// Renew owns its command until actual return, even for a broken custom backend.
	<-g.gate
	g.mu.Lock()
	g.timer.Stop()
	g.mu.Unlock()
	if !g.exported.Load() {
		g.cleanupErr = g.manager.releaseOwner(g.key, g.owner)
	}
	g.unlink()
	if finish != nil {
		finish()
	}
	close(g.done)
}

// Release cancels protected work and waits for one owner-conditional cleanup.
// Caller cancellation only stops waiting. Repeated/concurrent releases share the
// same attempt and result. Ownership loss remains available from Err separately.
func (g *Guard) Release(ctx context.Context) error {
	if g == nil || g.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "lease release requires an initialized guard and context")
	}
	_ = g.Err() // Observe owner cancellation before recording explicit release.
	g.cancel(ErrReleased)
	select {
	case <-g.done:
		return g.cleanupErr
	default:
	}
	select {
	case <-g.done:
		return g.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (g *Guard) Done() <-chan struct{} {
	if g == nil {
		return nil
	}
	return g.done
}
