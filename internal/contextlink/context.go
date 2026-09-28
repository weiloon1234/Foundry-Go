// Package contextlink links cancellation across owners while retaining values
// from the operation context. Callers must release every returned link.
package contextlink

import (
	"context"
	"slices"
)

// Link preserves ctx values and lets any supplied parent cancel the operation.
// Every context must be non-nil. Release unregisters all parent callbacks.
// Err observes parent cancellation synchronously, even when an AfterFunc
// callback is still queued. Observing an error also closes the operation's Done.
func Link(ctx context.Context, parents ...context.Context) (context.Context, context.CancelFunc) {
	operation, cancel := context.WithCancel(ctx)
	if len(parents) == 0 {
		return operation, cancel
	}
	linked := &linkedContext{Context: operation, parents: slices.Clone(parents), cancel: cancel}
	stops := make([]func() bool, len(linked.parents))
	for i, parent := range linked.parents {
		stops[i] = context.AfterFunc(parent, cancel)
		// AfterFunc schedules asynchronously for an already canceled parent.
		if parent.Err() != nil {
			cancel()
		}
	}
	return linked, func() {
		for _, stop := range stops {
			stop()
		}
		cancel()
	}
}

type linkedContext struct {
	context.Context
	parents []context.Context
	cancel  context.CancelFunc
}

func (c *linkedContext) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	// AfterFunc runs asynchronously. Checking the operation's final result must
	// not report success after an owner canceled merely because its callback has
	// not been scheduled yet. Cancel closes Done before returning a non-nil Err.
	for _, parent := range c.parents {
		if parent.Err() != nil {
			c.cancel()
			break
		}
	}
	return c.Context.Err()
}
