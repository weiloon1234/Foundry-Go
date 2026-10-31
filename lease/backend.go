// Package lease supplies typed, time-limited ownership with explicit loss and cleanup.
package lease

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Name identifies a lease family; it is distinct from cache family names.
type Name string

// Key is an opaque adapter address. It cannot be confused with a cache entry.
type Key struct{ address keyaddress.Address }

// NewKey is for adapters. Applications acquire through a typed bound declaration.
func NewKey(namespace keyspace.Namespace, name Name, logical string) (Key, error) {
	a, err := keyaddress.New(namespace, string(name), logical)
	return Key{address: a}, err
}

// Validate checks that the key contains a valid namespaced address.
func (k Key) Validate() error { return k.address.Validate() }

// Namespace returns the application and environment scope of the key.
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }

// String returns the physical lease address.
func (k Key) String() string { return k.address.String(KeyFeature) }

// Duration bounds keep expiry finite and leave room for client validity margins.
const (
	MinDuration = 10 * time.Millisecond
	MaxDuration = 24 * time.Hour
)

// ValidateDuration rejects durations outside MinDuration through MaxDuration with fault.Invalid.
func ValidateDuration(ttl time.Duration) error {
	if ttl < MinDuration || ttl > MaxDuration {
		return safeerror.NewFault(fault.Invalid, "lease duration must be between 10ms and 24h")
	}
	return nil
}

// Backend atomically acquires an absent key, or renews/releases only its current
// owner. Expired ownership is absent. Errors must never be treated as contention.
// Implementations must honor cancellation, perform no implicit mutation retries,
// and bound stored owner reads. TTL begins when the authority accepts a mutation.
// Renew cannot recreate expired ownership. Callers own adapter lifecycle.
type Backend interface {
	// LeaseAcquire sets an absent key to owner for ttl and reports whether ownership was acquired.
	LeaseAcquire(context.Context, Key, Owner, time.Duration) (bool, error)
	// LeaseRenew extends ttl only while owner still owns an unexpired key.
	LeaseRenew(context.Context, Key, Owner, time.Duration) (bool, error)
	// LeaseRelease removes key only while owner still owns it and reports whether it was removed.
	LeaseRelease(context.Context, Key, Owner) (bool, error)
}

// ValidateOperation shares the adapter input boundary. Release passes MinDuration.
func ValidateOperation(ctx context.Context, key Key, owner Owner, ttl time.Duration) error {
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "lease requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	return ValidateDuration(ttl)
}

// ErrLost means ownership could no longer be confirmed within its validity window.
var ErrLost = safeerror.Sentinel(fault.Conflict, "lease ownership lost")

// ErrReleased marks explicit release of a guard's work context.
var ErrReleased = safeerror.Sentinel(fault.Closed, "lease released")

// KeyFeature is the physical address family owned by this adapter.
const KeyFeature keyspace.Feature = "lease"
