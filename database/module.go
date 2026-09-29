package database

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module registers a typed, prepared pool and starts it during application boot.
// The adapter factory is pure construction: it must not acquire resources or
// start goroutines. A fresh pool is constructed for each application using this
// declaration. Feature adapters may wrap this helper with their own configuration.
func Module(name foundation.ProviderID, key foundation.Key[*DB], adapter func() (Adapter, error), config PoolConfig, options ...Option) foundation.Module {
	options = slices.Clone(options)
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		if adapter == nil {
			return fault.New(fault.Invalid, "database module needs an adapter factory")
		}
		if err := foundation.Factory(r, key, func(foundation.Resolver) (*DB, error) {
			value, err := adapter()
			if err != nil {
				return nil, err
			}
			db, err := Prepare(value, config, options...)
			if err != nil {
				return nil, err
			}
			db.managedObservers = true
			db.clockBound = false
			return db, nil
		}); err != nil {
			return err
		}
		return registerObserverBinding(r, key)
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		db, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("pool", func(ctx context.Context) error {
			err := db.Close(ctx)
			// The application's caller may stop waiting, but lifecycle ownership
			// cannot finish until the underlying pool has actually drained.
			<-db.Done()
			return errors.Join(err, db.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, db.Close(ctx))
		}
		if err := db.bindRuntimeClock(r.Clock()); err != nil {
			return err
		}
		if err := db.bindRuntimeLogger(r.Logger()); err != nil {
			return err
		}
		return db.Start(ctx)
	}}
}
