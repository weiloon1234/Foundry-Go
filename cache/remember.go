package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Remember reads a snapshot or coalesces concurrent misses within this Store.
// The selected owner supplies the loader, context and TTL; followers do not run
// their loaders. Each result is decoded independently, including the owner's.
// A canceled follower stops waiting without canceling the owner. An owner waits
// for its loader to actually exit; callbacks must cooperate with cancellation.
// Owner failure reaches existing followers without automatic retry. The timeout
// includes reads, loading, writing and waiting. Limits fail with fault.Conflict.
//
// A fill rechecks storage after election, but is not a transaction with Put or
// Forget: overlapping writes may be replaced and forgotten entries repopulated.
// NewCoordinatedStore adds lease-protected publication across instances. Ordinary
// stores coordinate locally. Neither mode promises exactly-once loaders. Recursive loads
// must propagate the supplied context; detected fill cycles fail with fault.Cycle.
func (c Cache[K, V]) Remember(ctx context.Context, key K, ttl TTL, loader func(context.Context) (V, error)) (V, error) {
	if loader == nil {
		return *new(V), fault.New(fault.Invalid, "cache Remember requires a loader")
	}
	if err := ttl.Validate(); err != nil {
		return *new(V), err
	}

	var value V
	err := c.execute(ctx, key, func(ctx context.Context, access entryAccess) error {

		data, found, err := c.read(ctx, access)
		if err != nil {
			return err
		}
		if !found {
			data, err = c.rememberMiss(ctx, access, ttl, loader)
			if err != nil {
				return err
			}
		}
		value, err = c.decode(ctx, data)
		return err
	})
	if err != nil {
		return *new(V), err
	}
	return value, nil
}

func (c Cache[K, V]) rememberMiss(ctx context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error)) ([]byte, error) {
	flight, owner, err := c.store.joinFill(ctx, access.fillKey)
	if err != nil {
		return nil, err
	}
	if !owner {
		return c.store.waitFill(ctx, flight)
	}
	// Nested isolation ensures backend/loader/codec Goexit also publishes failure
	// and releases the flight before the owning Remember call can return.
	var data []byte
	err = callback.Isolated("cache fill", func() error {
		loadContext := context.WithValue(ctx, fillContextKey{}, &fillContext{flight: flight, parent: currentFill(ctx), key: access.fillKey, distributed: c.store.coordination != nil})
		data, err = c.loadFill(loadContext, access, ttl, loader)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	c.store.finishFill(access.fillKey, flight, data, err)
	return data, err
}
