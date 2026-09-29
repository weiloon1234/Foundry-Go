package idempotency

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"slices"
)

// Module owns admission and drains active operations before its borrowed database
// provider shuts down. It starts the store's expiry pruner (Config.PruneInterval)
// after registering cleanup, logging failures through the application logger
// unless the store was constructed WithLogger.
func Module(name foundation.ProviderID, key foundation.Key[*Store], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Store, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid("idempotency module requires a constructor")
		}
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Store, error) {
			store, err := construct(r)
			if err != nil {
				return nil, err
			}
			if err := store.Validate(); err != nil {
				return nil, err
			}
			return store, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		store, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("idempotency", func(ctx context.Context) error {
			err := store.Close(ctx)
			<-store.Done()
			return errors.Join(err, store.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, store.Close(ctx))
		}
		return store.start(ctx, r.Logger())
	}}
}
