// Package cachestore shares maintenance state through an existing configured
// cache store. Redis and PostgreSQL stores provide fleet-wide state; memory and
// file stores are limited to the processes sharing that store instance/path.
package cachestore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

type recordKey string

const current recordKey = "current"

// Name is the cache family owning the shared maintenance record.
const Name cache.Name = "foundry.maintenance"

var declaration = cache.Define(Name, cache.StringKeys[recordKey](), cache.JSON[maintenance.State]())

// Store borrows a cache store; its owner must outlive every Load and Save.
// The record is persistent (no TTL). Choose a store whose backend does not
// evict persistent entries under memory pressure.
type Store struct {
	values cache.Cache[recordKey, maintenance.State]
}

var _ maintenance.Store = (*Store)(nil)

func New(store *cache.Store) (*Store, error) {
	if store == nil {
		return nil, fault.New(fault.Invalid, "maintenance cache store is missing")
	}
	values, err := declaration.Bind(store)
	if err != nil {
		return nil, err
	}
	return &Store{values: values}, nil
}

func (s *Store) Load(ctx context.Context) (maintenance.State, bool, error) {
	if s == nil {
		return maintenance.State{}, false, fault.New(fault.Invalid, "maintenance cache store is not initialized")
	}
	return s.values.Get(ctx, current)
}

func (s *Store) Save(ctx context.Context, state maintenance.State) error {
	if s == nil {
		return fault.New(fault.Invalid, "maintenance cache store is not initialized")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	return s.values.Put(ctx, current, state, cache.Forever())
}
