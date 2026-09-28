package infrastructure

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"slices"
)

// ownedAdapter centralizes rollback and teardown registration for prepared
// adapters. Feature modules retain their own operation-draining contracts.
type ownedAdapter[T any] struct {
	value        T
	start, close func(context.Context) error
}

func adapterModule[T any](name foundation.ProviderID, key foundation.Key[*ownedAdapter[T]], requires []foundation.ProviderID, construct func(foundation.Resolver) (*ownedAdapter[T], error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, construct)
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		adapter, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if adapter == nil {
			return fault.New(fault.Invalid, "configured adapter is missing")
		}
		if adapter.close != nil {
			if err := r.OnShutdown("adapter", adapter.close); err != nil {
				return errors.Join(err, adapter.close(ctx))
			}
		}
		if adapter.start != nil {
			return adapter.start(ctx)
		}
		return nil
	}}
}
