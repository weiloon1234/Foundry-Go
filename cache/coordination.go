package cache

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// CoordinationConfig sets the renewable fill lease and maximum contention wait.
// Cache Config.Timeout still bounds the entire Remember call, including loading.
type CoordinationConfig struct{ LeaseDuration, Wait time.Duration }

func DefaultCoordinationConfig() CoordinationConfig {
	return CoordinationConfig{LeaseDuration: 30 * time.Second, Wait: 2 * time.Second}
}

type coordinator struct {
	leases lease.Leases[EntryKey]
	config CoordinationConfig
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
func NewCoordinatedStore(manager *lease.Manager, config Config, coordination CoordinationConfig) (*Store, error) {
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
	store, err := NewStore(backend, config)
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
	store.coordination = &coordinator{leases: locks, config: coordination}
	return store, nil
}
func (c Cache[K, V]) loadFill(ctx context.Context, access entryAccess, ttl TTL, loader func(context.Context) (V, error)) ([]byte, error) {
	load := func(ctx context.Context, publish func(context.Context, []byte, TTL) error) ([]byte, error) {
		data, found, err := c.read(ctx, access)
		if err != nil || found {
			return data, err
		}
		loaded, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		data, err = c.encode(ctx, loaded, ttl)
		if err != nil {
			return nil, err
		}
		return data, publish(ctx, data, ttl)
	}
	if c.store.coordination == nil {
		return load(ctx, func(ctx context.Context, data []byte, ttl TTL) error {
			return access.backend.Put(ctx, access.key, data, ttl)
		})
	}
	backend, ok := access.backend.(CoordinatedBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "cache view does not support coordinated publication")
	}
	owner := c.store.coordination
	var data []byte
	ran, err := owner.leases.WithProof(ctx, access.fillKey, owner.config.LeaseDuration, owner.config.Wait, func(ctx context.Context, proof lease.Proof) error {
		var err error
		data, err = load(ctx, func(ctx context.Context, data []byte, ttl TTL) error {
			return backend.PutLeased(ctx, access.key, data, ttl, proof)
		})
		return err
	})
	if err == nil && !ran {
		err = fault.New(fault.Conflict, "cache fill lease is contended")
	}
	return data, err
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
