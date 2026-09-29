package cache

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/faultwrap"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
)

// RememberOption narrows one Remember call.
type RememberOption func(*rememberSettings) error

type rememberSettings struct {
	loadTimeout time.Duration
	// freshness, when set, makes a fill also require and publish the Flexible
	// freshness marker (see Flexible).
	freshness *freshness
	// background marks a fill nobody waits for; its failure is logged.
	background bool
}

// WithLoadTimeout bounds this call's loader instead of Config.LoadTimeout. The
// duration must be positive and at most MaxLoadTimeout. Store Config.Timeout
// never caps a loader; it bounds each backend step around it.
func WithLoadTimeout(timeout time.Duration) RememberOption {
	return func(s *rememberSettings) error {
		if timeout <= 0 || timeout > MaxLoadTimeout {
			return fault.New(fault.Invalid, "cache load timeout must be positive and bounded")
		}
		s.loadTimeout = timeout
		return nil
	}
}

// Remember reads a snapshot or coalesces concurrent misses within this Store.
// The selected owner supplies the loader and TTL; followers do not run their
// loaders. Each result is decoded independently, including the owner's.
//
// The loader runs in a fill owned by the Store, under a context that keeps the
// owner's values but not its cancellation, bounded by LoadTimeout (or
// WithLoadTimeout). A canceled caller, owner or follower, stops only its own
// wait; the fill finishes for everyone else and keeps its MaxFills slot until
// the loader actually exits. If a loader fails only because it used the
// owner's own (ended) request context, followers elect a new owner instead of
// failing. Other loader failures reach existing followers without retry.
//
// Config.Timeout bounds each backend step, not the loader. A failed publication
// of a successfully loaded value does not fail the call: every caller receives
// the value, and the failure is counted in Stats and logged (WithLogger). When
// MaxFills is exhausted, the caller loads directly without coalescing rather
// than failing; a full MaxFillWaiters returns retryable fault.Overloaded.
//
// A fill rechecks storage after election, but is not a transaction with Put or
// Forget: overlapping writes may be replaced and forgotten entries repopulated.
// NewCoordinatedStore adds lease-protected publication across instances. Ordinary
// stores coordinate locally. Neither mode promises exactly-once loaders. Recursive loads
// must propagate the supplied context; detected fill cycles fail with fault.Cycle.
func (c Cache[K, V]) Remember(ctx context.Context, key K, ttl TTL, loader func(context.Context) (V, error), options ...RememberOption) (V, error) {
	if loader == nil {
		return *new(V), fault.New(fault.Invalid, "cache Remember requires a loader")
	}
	if err := ttl.Validate(); err != nil {
		return *new(V), err
	}
	if err := c.valid(ctx); err != nil {
		return *new(V), err
	}
	started := c.startedAt()
	settings := rememberSettings{loadTimeout: c.store.config.LoadTimeout}
	for _, option := range options {
		if option == nil {
			return *new(V), fault.New(fault.Invalid, "cache Remember option is nil")
		}
		if err := option(&settings); err != nil {
			return *new(V), err
		}
	}

	var value V
	var found bool
	var access entryAccess
	var memo memoView
	var address EntryKey
	err := c.operation(ctx, func(ctx context.Context) error {
		var err error
		if address, err = c.address(key); err != nil {
			return err
		}
		if memo, err = c.memoView(ctx); err != nil {
			return err
		}
		// A memoized miss still reads the Store: a fill needs its snapshot.
		if entry, ok := memo.load(address); ok && entry.found {
			found = true
			value, err = c.decode(ctx, slices.Clone(entry.data))
			return err
		}
		observed, err := c.observe(ctx, address, true)
		if err != nil {
			return err
		}
		access, found = observed.access, observed.found
		if !found {
			return nil
		}
		memo.save(address, observed.data, true)
		value, err = c.decode(ctx, observed.data)
		return err
	})
	if err != nil {
		c.report(ctx, Event{Operation: OperationRemember}, started, err)
		return *new(V), err
	}
	c.store.countRead(found)
	if found {
		c.report(ctx, readEvent(OperationRemember, true), started, nil)
		return value, nil
	}
	event := Event{Operation: OperationRemember, Misses: 1, Loaded: true}
	data, outcome, err := c.rememberMiss(ctx, access, ttl, loader, settings)
	event.Unpublished = outcome.unpublished
	if err != nil {
		err = faultwrap.Wrap("cache operation failed", err)
		c.report(ctx, event, started, err)
		return *new(V), err
	}
	memo.save(address, data, true)
	// Fill results are shared by every waiter; each decodes its own copy.
	err = c.operation(ctx, func(ctx context.Context) error {
		var err error
		value, err = c.decode(ctx, slices.Clone(data))
		return err
	})
	c.report(ctx, event, started, err)
	if err != nil {
		return *new(V), err
	}
	return value, nil
}

func (c Cache[K, V]) rememberMiss(ctx context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error), settings rememberSettings) ([]byte, fillOutcome, error) {
	for attempt := 1; ; attempt++ {
		flight, role, err := c.store.joinFill(ctx, access.fillKey)
		if err != nil {
			return nil, fillOutcome{}, err
		}
		switch role {
		case fillUncoalesced:
			c.store.counters.uncoalesced.Add(1)
			return c.loadDirect(ctx, access, ttl, loader, settings)
		case fillOwner:
			c.startFill(ctx, access, flight, ttl, loader, settings)
		default:
			c.store.counters.coalesced.Add(1)
		}
		data, outcome, err := c.store.waitFill(ctx, flight, role == fillFollower)
		if outcome.retry && attempt < fillAttempts && ctx.Err() == nil {
			continue
		}
		return data, outcome, err
	}
}

// fillContext records the fill chain so a nested load of the same key is a
// cycle. A detached fill keeps the caller's values but not its cancellation;
// every fill is canceled when its Store closes. release ends the link.
func (c Cache[K, V]) fillContext(ctx context.Context, access entryAccess, detached bool) (context.Context, func()) {
	chain := &fillContext{store: c.store, parent: currentFill(ctx), key: access.fillKey, distributed: c.store.coordination != nil}
	if detached {
		ctx = context.WithoutCancel(ctx)
	}
	return contextlink.Link(context.WithValue(ctx, fillContextKey{}, chain), c.store.lifetime)
}

// startFill runs the owner's loader in a Store-owned goroutine. The registry
// entry (and MaxFills slot) is released only when the loader actually exits,
// including after panic or runtime.Goexit.
func (c Cache[K, V]) startFill(owner context.Context, access entryAccess, flight *fill, ttl TTL, loader func(context.Context) (V, error), settings rememberSettings) {
	fillCtx, release := c.fillContext(owner, access, true)
	go func() {
		var data []byte
		var outcome fillOutcome
		var err error = fault.New(fault.Panicked, "cache fill exited without returning")
		defer func() {
			release()
			c.store.finishFill(access.fillKey, flight, data, err, outcome)
		}()
		err = callback.Invoke("cache fill", func() error {
			var err error
			data, err = c.fill(fillCtx, owner, access, ttl, loader, settings, &outcome)
			return err
		})
		if err != nil && settings.background {
			c.store.recordFailure(fillCtx, "cache background refresh failed", c.definition.name, err)
		}
	}()
}

// loadDirect loads without coalescing when the fill registry is full. It still
// rechecks storage, publishes with the store's normal protection and reports
// publication failures without failing the caller.
// It stays in the fill chain (so a self-recursive loader fails with Cycle) and
// counts as running until it exits, so Close drains it too.
func (c Cache[K, V]) loadDirect(ctx context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error), settings rememberSettings) ([]byte, fillOutcome, error) {
	defer c.store.endDirect()
	fillCtx, release := c.fillContext(ctx, access, false)
	defer release()
	var data []byte
	var outcome fillOutcome
	err := callback.Isolated("cache fill", func() error {
		var err error
		data, err = c.fill(fillCtx, nil, access, ttl, loader, settings, &outcome)
		return err
	})
	return data, outcome, err
}

// step bounds one backend interaction around a loader by Config.Timeout.
func (c Cache[K, V]) step(ctx context.Context, fn func(context.Context) error) error {
	bounded, cancel := context.WithTimeout(ctx, c.store.config.Timeout)
	defer cancel()
	if err := fn(bounded); err != nil {
		return err
	}
	return bounded.Err()
}

// recheck reads the fill's own snapshot. A snapshot replaced by a concurrent
// invalidation is a miss here; the stale publication is then reported, not returned.
func (c Cache[K, V]) recheck(ctx context.Context, access entryAccess) ([]byte, bool, error) {
	var data []byte
	var found bool
	err := c.step(ctx, func(ctx context.Context) error {
		var err error
		data, found, err = c.read(ctx, access)
		return err
	})
	if err != nil && ctx.Err() == nil && errorgraph.Is(err, fault.Conflict) {
		return nil, false, nil
	}
	return data, found, err
}

// load runs one loader attempt: recheck, load, encode and publish. owner is the
// electing caller's context (nil for a direct load); outcome records a failure
// caused only by that ended context and a publication that did not store.
func (c Cache[K, V]) load(ctx, owner context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error), settings rememberSettings, outcome *fillOutcome, publish func(context.Context, []byte, TTL) error) ([]byte, error) {
	data, found, err := c.current(ctx, access, settings)
	if err != nil || found {
		return data, err
	}
	loadCtx, cancel := context.WithTimeout(ctx, settings.loadTimeout)
	defer cancel()
	var loaded V
	err = callback.Isolated("cache loader", func() error {
		c.store.counters.loads.Add(1)
		var err error
		loaded, err = loader(loadCtx)
		if err != nil {
			return err
		}
		data, err = c.encode(loadCtx, loaded, ttl)
		return err
	})
	if err != nil {
		if owner != nil && loadCtx.Err() == nil && owner.Err() != nil {
			_ = callback.Invoke("classify cache loader failure", func() error {
				outcome.retry = errorgraph.Is(err, owner.Err())
				return nil
			})
		}
		return nil, err
	}
	err = c.step(ctx, func(ctx context.Context) error {
		// Application adapters stay isolated so even a panic or Goexit while
		// publishing is a reported cache failure, not a failed request.
		return frameworkadapter.Call(c.store.backend, "cache publication", func() error { return publish(ctx, data, ttl) })
	})
	if err != nil {
		// The loaded value is still correct for this call; a cache failure must
		// not fail the request. Report it instead of retrying the write.
		c.store.counters.writeFailures.Add(1)
		outcome.unpublished = true
		c.store.recordFailure(ctx, "cache fill publication failed", c.definition.name, err)
		return data, nil
	}
	c.store.counters.writes.Add(1)
	c.markFresh(ctx, settings)
	return data, nil
}

// current rechecks the fill's value. A Flexible fill also requires its
// freshness marker, so a stale value is refreshed rather than returned.
func (c Cache[K, V]) current(ctx context.Context, access entryAccess, settings rememberSettings) ([]byte, bool, error) {
	data, found, err := c.recheck(ctx, access)
	if err != nil || !found || settings.freshness == nil {
		return data, found, err
	}
	_, fresh, err := c.recheck(ctx, settings.freshness.marker)
	if err != nil || !fresh {
		return nil, false, err
	}
	return data, true, nil
}
