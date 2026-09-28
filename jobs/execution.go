package jobs

import (
	"context"
	"reflect"
	"sync/atomic"
)

// Attempt is immutable handler metadata. ID is stable across worker redelivery;
// Number counts started executions and is not an exactly-once guarantee.
// No reservation owner is exposed to application handlers.
type Attempt struct {
	ID      ExecutionID
	Name    Name
	Version Version
	Queue   Queue
	Number  uint32
}
type executionFrame struct {
	worker  *Worker
	typ     reflect.Type
	attempt Attempt
	active  atomic.Bool
	noRetry atomic.Bool
}
type executionKey struct{}

// PreventRetry prevents automatic retry of this live attempt after a known
// external side effect. A later timeout or middleware failure still records a
// failed job; it does not pretend the whole job succeeded. The marker survives
// callback classification but not process/lease loss before finalization.
// Returns false outside a live started attempt, including saved contexts.
func PreventRetry(ctx context.Context) bool {
	if _, ok := Current(ctx); !ok {
		return false
	}
	frame, _ := ctx.Value(executionKey{}).(*executionFrame)
	frame.noRetry.Store(true)
	return true
}

// Current reports a live framework-owned job execution. A saved context does
// not continue representing an active execution after its handler has exited.
func Current(ctx context.Context) (Attempt, bool) {
	if ctx == nil {
		return Attempt{}, false
	}
	frame, _ := ctx.Value(executionKey{}).(*executionFrame)
	if frame == nil || !frame.active.Load() || frame.attempt.Number == 0 {
		return Attempt{}, false
	}
	return frame.attempt, true
}

// CurrentID keeps the payload owner for job-specific idempotency storage.
func (d Definition[P]) CurrentID(ctx context.Context) (ID[P], bool) {
	attempt, ok := Current(ctx)
	if ok {
		frame, _ := ctx.Value(executionKey{}).(*executionFrame)
		ok = frame.typ == reflect.TypeFor[P]()
	}
	if !ok || attempt.Name != d.name || attempt.Version != d.version {
		return ID[P]{}, false
	}
	// Type ownership is restored only at the registered descriptor boundary.
	return idFromExecution[P](attempt.ID), true
}
