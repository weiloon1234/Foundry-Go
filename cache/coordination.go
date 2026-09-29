package cache

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// CoordinationConfig sets the renewable fill lease and the maximum time a miss
// waits while another instance fills (polling both the lease and the cache at
// the lease manager's PollInterval). Loaders are bounded by LoadTimeout.
type CoordinationConfig struct{ LeaseDuration, Wait time.Duration }

func DefaultCoordinationConfig() CoordinationConfig {
	return CoordinationConfig{LeaseDuration: 30 * time.Second, Wait: 2 * time.Second}
}

type coordinator struct {
	leases lease.Leases[EntryKey]
	config CoordinationConfig
	poll   time.Duration
}

// One declaration identity is reused by every coordinated store sharing a manager.
var cacheFills = lease.Define("foundry.cache.fills.v1", keyspace.NewCodec(fillText))

func fillText(key EntryKey) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	return key.String(), nil
}

// NewCoordinatedStore enables distributed Remember before declarations are bound.
// The borrowed manager supplies both lease and cache capabilities from the SAME
// adapter/namespace. It performs no I/O and never falls back to an ordinary store.
// Close the lease manager before its adapter. Existing Cache methods remain typed.
func NewCoordinatedStore(manager *lease.Manager, config Config, coordination CoordinationConfig, options ...StoreOption) (*Store, error) {
	if err := manager.ValidateScope(coordination.LeaseDuration, coordination.Wait); err != nil {
		return nil, err
	}
	if config.Namespace != manager.Namespace() {
		return nil, fault.New(fault.Invalid, "cache and lease namespaces must match")
	}
	backend, ok := manager.BorrowedBackend().(CoordinatedBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "lease authority does not support coordinated cache publication")
	}
	store, err := NewStore(backend, config, options...)
	if err != nil {
		return nil, err
	}
	if _, scoped := backend.(TaggedBackend); scoped {
		if _, ok := backend.(CoordinatedTaggedBackend); !ok {
			return nil, fault.New(fault.Invalid, "cache backend requires coordinated tags for namespace protection")
		}
	}
	locks, err := cacheFills.Bind(manager)
	if err != nil {
		return nil, err
	}
	store.coordination = &coordinator{leases: locks, config: coordination, poll: manager.PollInterval()}
	return store, nil
}

// fill publishes through the store's protection. A coordinated store takes the
// distributed fill lease; while another instance owns it, the fill re-reads the
// cache between lease polls, so a value published elsewhere is returned as soon
// as it appears instead of waiting for the lease or failing on a slow loader.
func (c Cache[K, V]) fill(ctx, owner context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error), settings rememberSettings, outcome *fillOutcome) ([]byte, error) {
	if c.store.coordination == nil {
		return c.load(ctx, owner, access, ttl, loader, settings, outcome, func(ctx context.Context, data []byte, ttl TTL) error {
			return access.backend.Put(ctx, access.key, data, ttl)
		})
	}
	backend, ok := access.backend.(CoordinatedBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "cache view does not support coordinated publication")
	}
	coordination := c.store.coordination
	deadline := time.Now().Add(coordination.config.Wait)
	for {
		var data []byte
		loaded := false
		ran, err := coordination.leases.WithProof(ctx, access.fillKey, coordination.config.LeaseDuration, 0, func(ctx context.Context, proof lease.Proof) error {
			var err error
			data, err = c.load(ctx, owner, access, ttl, loader, settings, outcome, func(ctx context.Context, data []byte, ttl TTL) error {
				return backend.PutLeased(ctx, access.key, data, ttl, proof)
			})
			loaded = err == nil
			return err
		})
		if loaded {
			// The value is correct for this call even if losing or releasing the
			// fill lease failed afterwards; the manager retains cleanup errors.
			if err != nil {
				c.store.recordFailure(ctx, "cache fill lease release failed", c.definition.name, err)
			}
			return data, nil
		}
		if err != nil || ran {
			return data, err
		}
		// Another instance is filling: observe its publication directly.
		data, found, err := c.current(ctx, access, settings)
		if err != nil || found {
			return data, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if coordination.config.Wait == 0 {
				return nil, fault.New(fault.Conflict, "cache fill lease is contended")
			}
			return nil, fault.Wrap(fault.Timeout, "cache fill lease wait expired", context.DeadlineExceeded)
		}
		delay := coordination.poll/2 + time.Duration(rand.Int64N(int64(coordination.poll-coordination.poll/2)))
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// FillLeaseKey is the adapter address for a fill identity. Tagged callers use
// TaggedKey.FillKey, so a new tag generation has independent ownership.
func FillLeaseKey(fill EntryKey) (lease.Key, error) {
	logical, err := fillText(fill)
	if err != nil {
		return lease.Key{}, err
	}
	return lease.NewKey(fill.Namespace(), cacheFills.Name(), logical)
}

// ValidateFillProof checks cancellation, local validity and the exact cache-fill
// binding. Adapters must additionally compare ownership atomically at publication.
func ValidateFillProof(ctx context.Context, fill EntryKey, proof lease.Proof) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "cache fill publication requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := proof.Validate(); err != nil {
		return err
	}
	expected, err := FillLeaseKey(fill)
	if err != nil {
		return err
	}
	if expected != proof.Key() {
		return fault.New(fault.Invalid, "lease proof belongs to a different cache fill")
	}
	return nil
}
