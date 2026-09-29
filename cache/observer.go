package cache

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/faultwrap"
)

// Operation identifies a typed cache call reported to an Observer.
type Operation uint8

const (
	OperationGet Operation = iota + 1
	OperationGetMany
	OperationRemember
	OperationPut
	OperationPutMany
	OperationAdd
	OperationForget
	OperationForgetMany
	OperationIncrement
	OperationExists
	OperationExpire
	OperationInvalidate
	OperationFlexible
)

var operationNames = [...]string{OperationGet: "get", OperationGetMany: "get_many", OperationRemember: "remember", OperationPut: "put", OperationPutMany: "put_many", OperationAdd: "add", OperationForget: "forget", OperationForgetMany: "forget_many", OperationIncrement: "increment", OperationExists: "exists", OperationExpire: "expire", OperationInvalidate: "invalidate", OperationFlexible: "flexible"}

// String returns a stable snake_case name suitable for metric labels.
func (o Operation) String() string {
	if int(o) < len(operationNames) && operationNames[o] != "" {
		return operationNames[o]
	}
	return "unknown"
}

// Event describes one completed typed cache call. Family is the declaration
// name; it is empty for Store.Invalidate and Store.InvalidateTags. Hits and
// Misses count the keys a read observed (Get, GetMany, Exists and the lookup
// inside Remember and Flexible). Loaded reports a miss that ran or joined a
// loader, and Unpublished that the loaded value could not be stored (the caller
// still received it). Stale reports a Flexible hit served while a background
// refresh was requested. Code is the framework classification of a failure and
// is empty on success. Keys, values and error text are never exposed.
type Event struct {
	Family       Name
	Operation    Operation
	Hits, Misses int
	Loaded       bool
	Unpublished  bool
	Stale        bool
	Duration     time.Duration
	Code         fault.Code
}

// Observer receives completed calls synchronously on the calling goroutine. It
// must be fast, must not use the cache and must not retain the context. A panic
// is contained and never changes the operation's result.
type Observer func(context.Context, Event)

// WithObserver installs one call observer for metrics or tracing. Observers are
// fixed for the Store's lifetime.
func WithObserver(observer Observer) StoreOption {
	return func(s *Store) error {
		if observer == nil {
			return fault.New(fault.Invalid, "cache observer is nil")
		}
		if s.observer != nil {
			return fault.New(fault.Invalid, "cache observer is already configured")
		}
		s.observer = observer
		return nil
	}
}

// started returns the start instant of an observed call, or zero without an
// observer so unobserved calls do not read the clock.
func (s *Store) started() time.Time {
	if s == nil || s.observer == nil {
		return time.Time{}
	}
	return time.Now()
}

// report delivers one completed call. A failed call reports its framework code.
func (s *Store) report(ctx context.Context, event Event, started time.Time, err error) {
	if s == nil || s.observer == nil || ctx == nil {
		return
	}
	event.Duration = time.Since(started)
	if err != nil {
		event.Code = faultwrap.Code(err)
	}
	_ = callback.Invoke("cache observer", func() error {
		s.observer(ctx, event)
		return nil
	})
}

// report sends this handle's completed call with its declaration name.
func (c Cache[K, V]) report(ctx context.Context, event Event, started time.Time, err error) {
	if c.store == nil || c.definition == nil {
		return
	}
	event.Family = c.definition.name
	c.store.report(ctx, event, started, err)
}

// startedAt is Store.started for a handle; zero-value handles report nothing.
func (c Cache[K, V]) startedAt() time.Time { return c.store.started() }

// readEvent reports one key read as a hit or miss.
func readEvent(operation Operation, hit bool) Event {
	if hit {
		return Event{Operation: operation, Hits: 1}
	}
	return Event{Operation: operation, Misses: 1}
}
