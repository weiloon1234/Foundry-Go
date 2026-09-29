package events

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Dispatch captures a typed payload and runs registered listeners in order.
// Each listener receives a fresh decoded value. Failure stops later listeners;
// successful earlier side effects are not automatically reversed. Use the
// supplied transaction explicitly when listener work needs rollback semantics.
func (t Topic[E]) Dispatch(ctx context.Context, bus *Bus, input E) error {
	operation, entry, release, err := bus.begin(ctx, topicKey{t.name, t.version}, reflect.TypeFor[E]())
	if err != nil {
		return err
	}
	defer release()
	captured, err := t.captureOwned(operation, input)
	if err != nil {
		return err
	}
	return dispatch(operation, bus, entry, captured)
}

func (t Topic[E]) captureOwned(ctx context.Context, input E) (payload, error) {
	if err := ctx.Err(); err != nil {
		return payload{}, err
	}
	var captured payload
	err := callback.Isolated("capture event payload", func() error {
		var err error
		captured, err = t.capture(input)
		return err
	})
	if err != nil {
		return payload{}, err
	}
	if err := ctx.Err(); err != nil {
		return payload{}, err
	}
	return captured, nil
}

// dispatch runs sync listeners inline and enqueues queued listeners as jobs, in
// declaration order. An installed interceptor (a test fake) may suppress them.
func dispatch(ctx context.Context, bus *Bus, entry registration, captured payload) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if captured.typ != entry.schema.typ {
		return fault.New(fault.Internal, "captured event has the wrong payload schema")
	}
	if bus.intercepted(ctx, entry.schema.key, captured) {
		return ctx.Err()
	}
	for _, listener := range entry.listeners {
		if err := ctx.Err(); err != nil {
			return err
		}
		if listener.queued {
			if err := bus.enqueueListener(ctx, entry.schema.key, listener.name, captured.text); err != nil {
				return err
			}
			continue
		}
		err := callback.Isolated(fmt.Sprintf("event listener %s", listener.name), func() error { return listener.invoke(ctx, captured) })
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// AfterCommit snapshots payload and attribution now, then schedules process-local
// dispatch through the actual transaction. Rollback suppresses dispatch. Execution
// uses the outer commit callback context, not a retained per-write context.
// Listener failure is reported as committed AfterCommitFailed by the database.
// This has no crash durability; durable publication requires an outbox row.
func (t Topic[E]) AfterCommit(ctx context.Context, tx *database.Tx, bus *Bus, input E) error {
	if tx == nil {
		return fault.New(fault.Invalid, "after-commit event requires a transaction")
	}
	operation, _, release, err := bus.begin(ctx, topicKey{t.name, t.version}, reflect.TypeFor[E]())
	if err != nil {
		return err
	}
	defer release()
	origin := attribution.FromContext(operation)
	if err := origin.Validate(); err != nil {
		return err
	}
	captured, err := t.captureOwned(operation, input)
	if err != nil {
		return err
	}
	key, typ := topicKey{t.name, t.version}, reflect.TypeFor[E]()
	return tx.AfterCommit(func(commitContext context.Context) error {
		restored, err := attribution.WithContext(commitContext, origin)
		if err != nil {
			return err
		}
		operation, entry, release, err := bus.begin(restored, key, typ)
		if err != nil {
			return err
		}
		defer release()
		return dispatch(operation, bus, entry, captured)
	})
}
