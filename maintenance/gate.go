// Package maintenance controls admission while existing work retains its normal
// resource ownership and cancellation rules. It owns no servers or goroutines.
package maintenance

import (
	"context"
	"crypto/rand"
	"errors"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type Mode string

const (
	Serving  Mode = "serving"
	Paused   Mode = "maintenance"
	Draining Mode = "draining"
)

var ErrMaintenance = errors.New("application is in maintenance mode")
var ErrDraining = errors.New("application is draining")

// Gate is application-owned and concurrency-safe. Pausing admission is
// reversible; Drain is terminal. A nil optional gate represents normal serving.
// Admission checks do not cancel existing work or revoke acquired resources.
// Do not copy a Gate after first use. The zero value is ready to use and has
// an empty Policy; New validates and snapshots configured exemptions.
type Gate struct {
	mu      sync.Mutex
	mode    Mode
	changed chan struct{}
	policy  Policy
	state   State
	// localKey signs bypass cookies when Policy.Keys is nil; it is generated
	// on first use and never leaves this process.
	localKey []byte
}

// New constructs a serving gate retaining a validated Policy snapshot.
func New(policy Policy) (*Gate, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Gate{policy: policy.snapshot()}, nil
}

// Policy returns an owned snapshot of the configured exemptions.
func (g *Gate) Policy() Policy {
	if g == nil {
		return Policy{}.snapshot()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.policy.snapshot()
}

// State returns an owned snapshot of the applied operator state. Down reports
// whether admission is currently paused; a draining gate reports Down true.
func (g *Gate) State() State {
	if g == nil {
		return State{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	state := g.state.snapshot()
	state.Down = g.modeLocked() != Serving
	return state
}

// Apply replaces the operator state, pausing or resuming admission. A shared
// store applies the same record on every instance. Drain remains terminal.
func (g *Gate) Apply(state State) error {
	if g == nil {
		return fault.New(fault.Invalid, "maintenance changes require an application gate")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.modeLocked() == Draining {
		return ErrDraining
	}
	g.state = state.snapshot()
	want := Serving
	if state.Down {
		want = Paused
	}
	if g.modeLocked() != want {
		g.signalLocked(want)
	}
	return nil
}

// Exempts reports whether a paused gate admits this request through a
// configured or shared rule or allowed network. It is false unless the gate is
// paused, so exemptions never reopen a draining application.
func (g *Gate) Exempts(method, path string, ip netip.Addr) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.modeLocked() != Paused {
		return false
	}
	match := func(rule Rule) bool { return rule.Matches(method, path) }
	contains := func(prefix netip.Prefix) bool { return ip.IsValid() && prefix.Contains(ip.Unmap()) }
	return slices.ContainsFunc(g.policy.Exempt, match) || slices.ContainsFunc(g.state.Exempt, match) || slices.ContainsFunc(g.policy.Allow, contains) || slices.ContainsFunc(g.state.Allow, contains)
}

// Bypass reports whether a paused gate admits the holder of a signed cookie.
func (g *Gate) Bypass(ctx context.Context, cookie string, now time.Time) bool {
	if g == nil || ctx == nil {
		return false
	}
	g.mu.Lock()
	if g.modeLocked() != Paused || g.state.Secret.IsZero() {
		g.mu.Unlock()
		return false
	}
	keys, digest, local := g.policy.Keys, g.state.Secret, g.localKey
	g.mu.Unlock()
	if keys == nil && local == nil {
		return false // Nothing was issued by this process.
	}
	return openBypass(ctx, keys, local, digest, cookie, now)
}

// IssueBypass signs a bypass cookie when a paused gate's shared secret matches
// candidate. The cookie lifetime comes from the configured Policy.
func (g *Gate) IssueBypass(ctx context.Context, candidate string, now time.Time) (string, time.Time, bool) {
	if g == nil || ctx == nil {
		return "", time.Time{}, false
	}
	g.mu.Lock()
	if g.modeLocked() != Paused || !g.state.MatchesSecret(candidate) {
		g.mu.Unlock()
		return "", time.Time{}, false
	}
	if g.policy.Keys == nil && g.localKey == nil {
		g.localKey = make([]byte, 32)
		if _, err := rand.Read(g.localKey); err != nil {
			g.localKey = nil
			g.mu.Unlock()
			return "", time.Time{}, false
		}
	}
	keys, digest, local, ttl := g.policy.Keys, g.state.Secret, g.localKey, g.policy.snapshot().BypassTTL
	g.mu.Unlock()
	expires := now.Add(ttl).Truncate(time.Second)
	value, err := sealBypass(ctx, keys, local, digest, expires)
	return value, expires, err == nil
}

func (g *Gate) modeLocked() Mode {
	if g.mode == "" {
		return Serving
	}
	return g.mode
}

func (g *Gate) Mode() Mode {
	if g == nil {
		return Serving
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.modeLocked()
}

func (g *Gate) signalLocked(mode Mode) {
	g.mode = mode
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}

// Set pauses or resumes admission. Existing work continues under its original
// lifetime; application shutdown must still drain the owning kernels/resources.
func (g *Gate) Set(paused bool) error {
	if g == nil {
		return fault.New(fault.Invalid, "maintenance changes require an application gate")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.modeLocked() == Draining {
		return ErrDraining
	}
	want := Serving
	if paused {
		want = Paused
		g.state.Down = true
	} else {
		g.state = State{}
	}
	if g.modeLocked() != want {
		g.signalLocked(want)
	}
	return nil
}

// Drain permanently stops admission and wakes waiting workers. It is idempotent.
func (g *Gate) Drain() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.modeLocked() != Draining {
		g.signalLocked(Draining)
	}
}

type contextKey struct{}

// WithContext carries the application's admission gate. Kernels derive request,
// job and command contexts from the application runtime, so every admission
// check shares one gate even when observability is disabled. A nil gate clears
// an inherited gate and represents normal serving.
func WithContext(ctx context.Context, gate *Gate) context.Context {
	return context.WithValue(ctx, contextKey{}, gate)
}

// FromContext returns the carried gate, or nil (normal serving) when absent.
func FromContext(ctx context.Context) *Gate {
	if ctx == nil {
		return nil
	}
	gate, _ := ctx.Value(contextKey{}).(*Gate)
	return gate
}

func (g *Gate) Admit() error {
	switch g.Mode() {
	case Paused:
		return ErrMaintenance
	case Draining:
		return ErrDraining
	default:
		return nil
	}
}

// Wait holds new worker admission while paused. It owns no goroutine and wakes
// on resume, terminal drain or caller cancellation. HTTP admission should use
// Admit and respond promptly instead of holding a request until maintenance ends.
func (g *Gate) Wait(ctx context.Context) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "maintenance wait requires a context")
	}
	if g == nil {
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.mu.Lock()
		mode := g.modeLocked()
		if mode == Serving {
			g.mu.Unlock()
			return nil
		}
		if mode == Draining {
			g.mu.Unlock()
			return ErrDraining
		}
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
