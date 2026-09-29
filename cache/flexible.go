package cache

import (
	"context"
	"crypto/sha256"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/faultwrap"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
)

// freshness is the Flexible marker a fill must find and publish with its value.
type freshness struct {
	marker entryAccess
	ttl    TTL
}

// freshnessKey derives the reserved address that marks base as fresh. Its
// logical-key hash has a control-character preimage, which no application key
// can produce (logical keys exclude control characters).
func freshnessKey(base EntryKey) (EntryKey, error) {
	hash := sha256.Sum256(append([]byte("\x00foundry.cache.fresh.v1\x00"), base.address.Hash[:]...))
	address, err := keyaddress.FromHash(base.Namespace(), string(base.Name()), hash)
	return EntryKey{address: address}, err
}

// markFresh publishes a Flexible fill's freshness marker after its value. A
// failure only makes the value stale early; it is counted and logged.
func (c Cache[K, V]) markFresh(ctx context.Context, settings rememberSettings) {
	if settings.freshness == nil {
		return
	}
	marker := settings.freshness.marker
	err := c.step(ctx, func(ctx context.Context) error {
		return marker.backend.Put(ctx, marker.key, []byte{}, settings.freshness.ttl)
	})
	if err != nil {
		c.store.counters.writeFailures.Add(1)
		c.store.recordFailure(ctx, "cache freshness publication failed", c.definition.name, err)
	}
}

// Flexible is Remember with stale-while-revalidate. A value younger than fresh
// is returned as is. An older value, up to fresh+stale, is returned immediately
// while one Store-owned background fill (coalesced with other fills of the key)
// loads and publishes a replacement; its failure is only logged (WithLogger). A
// missing or expired value is loaded like Remember and stored for fresh+stale.
//
// Freshness is a separate marker entry written after the value with TTL fresh
// and read with it in one batch, so no clock comparison is involved. The marker
// is an ordinary stored entry: it counts toward adapter capacity (MaxEntries and
// MaxBytes of file and PostgreSQL stores) and invalidation removes it with the
// value. The value stays an ordinary entry of this family (Get, Forget and
// invalidation apply). Put, PutMany, Add, Increment and Expire leave the marker
// unchanged: a value written while a marker lives counts as fresh until that
// marker expires; without one it is served stale and refreshed. Loader, timeout,
// coalescing and publication rules are those of Remember. The background fill
// keeps the caller's values but not its cancellation. Flexible reads are not
// memoized by WithMemo.
func (c Cache[K, V]) Flexible(ctx context.Context, key K, fresh, stale time.Duration, loader func(context.Context) (V, error), options ...RememberOption) (V, error) {
	if loader == nil {
		return *new(V), fault.New(fault.Invalid, "cache Flexible requires a loader")
	}
	if fresh <= 0 || stale <= 0 || fresh > math.MaxInt64-stale {
		return *new(V), fault.New(fault.Invalid, "cache Flexible durations must be positive and bounded")
	}
	if err := c.valid(ctx); err != nil {
		return *new(V), err
	}
	started := c.startedAt()
	settings := rememberSettings{loadTimeout: c.store.config.LoadTimeout}
	for _, option := range options {
		if option == nil {
			return *new(V), fault.New(fault.Invalid, "cache Flexible option is nil")
		}
		if err := option(&settings); err != nil {
			return *new(V), err
		}
	}
	ttl := For(fresh + stale)
	var value V
	var found, current bool
	var access entryAccess
	var address EntryKey
	err := c.operation(ctx, func(ctx context.Context) error {
		var err error
		if address, err = c.address(key); err != nil {
			return err
		}
		markerAddress, err := freshnessKey(address)
		if err != nil {
			return err
		}
		bases := []EntryKey{address, markerAddress}
		slices.SortFunc(bases, func(a, b EntryKey) int { return strings.Compare(a.String(), b.String()) })
		values, snapshot, err := c.readBatchSnapshot(ctx, bases)
		if err != nil {
			return err
		}
		if access, err = snapshot.bind(address); err != nil {
			return err
		}
		marker, err := snapshot.bind(markerAddress)
		if err != nil {
			return err
		}
		settings.freshness = &freshness{marker: marker, ttl: For(fresh)}
		stored, mark := values[0], values[1]
		if bases[0] != address {
			stored, mark = mark, stored
		}
		if !stored.Found || len(stored.Data) > c.store.config.MaxValueBytes {
			return nil
		}
		value, err = c.decode(ctx, stored.Data)
		found, current = err == nil, mark.Found
		return err
	})
	if err != nil {
		c.report(ctx, Event{Operation: OperationFlexible}, started, err)
		return *new(V), err
	}
	c.store.countRead(found)
	if found {
		if !current {
			c.revalidate(ctx, access, ttl, loader, settings)
		}
		c.report(ctx, Event{Operation: OperationFlexible, Hits: 1, Stale: !current}, started, nil)
		return value, nil
	}
	event := Event{Operation: OperationFlexible, Misses: 1, Loaded: true}
	// The load may replace the entry: drop any memoized read of it.
	defer forgetMemo(ctx, c.store, address)
	data, outcome, err := c.rememberMiss(ctx, access, ttl, loader, settings)
	event.Unpublished = outcome.unpublished
	if err != nil {
		err = faultwrap.Wrap("cache operation failed", err)
		c.report(ctx, event, started, err)
		return *new(V), err
	}
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

// revalidate starts one background fill for a stale value without waiting. A
// running fill of the key already refreshes it; an exhausted registry, fill
// cycle or waiter limit skips the refresh (the next stale read retries).
func (c Cache[K, V]) revalidate(ctx context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error), settings rememberSettings) {
	flight, role, err := c.store.joinFill(ctx, access.fillKey)
	if err != nil {
		return
	}
	switch role {
	case fillOwner:
		settings.background = true
		c.startFill(ctx, access, flight, ttl, loader, settings)
	case fillFollower:
		c.store.leaveFill(flight)
	case fillUncoalesced:
		c.store.endDirect()
	}
}
