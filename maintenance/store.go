package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const (
	// DefaultPollInterval is how often an instance reloads the shared state.
	DefaultPollInterval = 5 * time.Second
	// MinPollInterval and MaxPollInterval bound shared-state polling.
	MinPollInterval = 100 * time.Millisecond
	MaxPollInterval = 10 * time.Minute
	maxLoadTimeout  = 5 * time.Second
)

// Store shares one operator State across a fleet, for example through a Redis
// or PostgreSQL cache store. Load reports found=false before any State was
// saved. Implementations must honor context cancellation and bound their I/O.
// Admission never reads the store: each instance applies polled records to its
// local Gate, which is the cache consulted on every request, job and command.
type Store interface {
	Load(context.Context) (State, bool, error)
	Save(context.Context, State) error
}

// Refresh loads the shared record once and applies it. An absent record leaves
// the local gate unchanged, so configured initial maintenance survives until an
// operator first saves shared state. Draining gates ignore the record.
func Refresh(ctx context.Context, gate *Gate, store Store) error {
	if ctx == nil || gate == nil || store == nil {
		return fault.New(fault.Invalid, "maintenance refresh requires a context, gate and store")
	}
	state, found, err := store.Load(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := gate.Apply(state); err != nil && !errors.Is(err, ErrDraining) {
		return err
	}
	return nil
}

// Publish saves shared state and then applies it to the local gate, so the
// issuing process observes the same record as polling instances.
func Publish(ctx context.Context, gate *Gate, store Store, state State) error {
	if ctx == nil || gate == nil || store == nil {
		return fault.New(fault.Invalid, "maintenance publication requires a context, gate and store")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	if err := store.Save(ctx, state); err != nil {
		return err
	}
	return gate.Apply(state)
}

// Watch refreshes the gate every interval until ctx ends and then returns nil.
// A failed load retains the last applied state; report receives the first
// failure after each success (it may be nil) so a store outage is visible
// without logging every poll. Transient failures never stop the application.
func Watch(ctx context.Context, gate *Gate, store Store, interval time.Duration, report func(error)) error {
	if ctx == nil || gate == nil || store == nil {
		return fault.New(fault.Invalid, "maintenance watch requires a context, gate and store")
	}
	if interval < MinPollInterval || interval > MaxPollInterval {
		return fault.New(fault.Invalid, "maintenance poll interval is out of range")
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	failing := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		load, cancel := context.WithTimeout(ctx, min(interval, maxLoadTimeout))
		err := Refresh(load, gate, store)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && !failing && report != nil {
			report(err)
		}
		failing = err != nil
		timer.Reset(interval)
	}
}
