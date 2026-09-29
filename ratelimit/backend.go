package ratelimit

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/ratewindow"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Name string

// Key is an opaque adapter address, distinct from cache and lease keys.
type Key struct{ address keyaddress.Address }

// NewKey is for adapters and tests. Applications use a typed Limiter.
func NewKey(namespace keyspace.Namespace, name Name, logical string) (Key, error) {
	address, err := keyaddress.New(namespace, string(name), logical)
	return Key{address}, err
}
func (k Key) Validate() error               { return k.address.Validate() }
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }
func (k Key) String() string                { return k.address.String("ratelimit") }

// WindowOffset is this key's deterministic window phase in [0, window). Adapters
// start fixed windows at offset + n*window, so the buckets of many keys do not
// all reset at the same Unix-epoch boundary. Every authority derives the same
// offset from the key address, independent of process or client clock. An
// uninitialized key or a nonpositive window has no phase.
func (k Key) WindowOffset(window time.Duration) time.Duration {
	if k.Validate() != nil {
		return 0
	}
	ms := window.Milliseconds()
	return time.Duration(ratewindow.Offset(ratewindow.Seed(k.address.Hash), ms)) * time.Millisecond
}

// Backend owns one authority's clock and atomic fixed-window decisions. A live
// bucket with a different policy is converted to the requested one: admitted
// usage still counts (capped at the new capacity) and a new window starts on the
// new policy's phase. Denials consume nothing; a denied take persists only a
// conversion that ends later than the live bucket, so carried usage is never
// released early. Windows use
// Key.WindowOffset. A backward clock step inside a live bucket is clamped to that
// bucket's start instead of reopening quota or failing. Implementations honor
// cancellation, bound metadata reads and never implicitly retry a mutation or
// fall back to another authority. The caller owns the adapter lifecycle; network
// errors may leave consumption unconfirmed.
type Backend interface {
	RateLimit(context.Context, Key, Limit, uint32) (Decision, error)
}

// InspectBackend is an optional Backend capability. PeekRateLimit reports the
// decision cost would receive now, using the same policy conversion and clock
// rules, without consuming capacity or changing expiry; see Decision.ValidatePeek.
// ClearRateLimit removes the key's bucket, including unreadable state at its
// address, so the next request starts a fresh window; true means state existed.
// Both follow Backend's cancellation, validation and no-retry rules.
type InspectBackend interface {
	PeekRateLimit(context.Context, Key, Limit, uint32) (Decision, error)
	ClearRateLimit(context.Context, Key) (bool, error)
}

func ValidateOperation(ctx context.Context, key Key, limit Limit, cost uint32) error {
	if err := ValidateClear(ctx, key); err != nil {
		return err
	}
	return limit.ValidateCost(cost)
}

// ValidateClear shares the adapter input boundary for ClearRateLimit.
func ValidateClear(ctx context.Context, key Key) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "rate limiting requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return key.Validate()
}
