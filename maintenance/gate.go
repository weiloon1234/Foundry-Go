// Package maintenance controls admission while existing work retains its normal
// resource ownership and cancellation rules. It owns no servers or goroutines.
package maintenance

import (
	"context"
	"errors"
	"sync"

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
// Do not copy a Gate after first use. The zero value is ready to use.
type Gate struct {
	mu      sync.Mutex
	mode    Mode
	changed chan struct{}
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
