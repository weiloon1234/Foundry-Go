package lease

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module builds one manager per application. requires identifies the provider(s)
// owning the borrowed backend, ensuring leases drain before those adapters close.
func Module(name foundation.ProviderID, key foundation.Key[*Manager], config Config, requires []foundation.ProviderID, backend func(foundation.Resolver) (Backend, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Manager, error) {
			if backend == nil {
				return NewManager(nil, config)
			}
			value, err := backend(r)
			if err != nil {
				return nil, err
			}
			return NewManager(value, config)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		manager, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("leases", func(ctx context.Context) error {
			err := manager.Close(ctx)
			<-manager.Done()
			return errors.Join(err, manager.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, manager.Close(ctx))
		}
		return nil
	}}
}
