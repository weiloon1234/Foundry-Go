package outbound

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module owns the service and requires the providers owning its borrowed
// database, keyring and HTTP client. Workers running delivery jobs must stop
// first; shutdown waits for in-flight operations to exit.
func Module(name foundation.ProviderID, key foundation.Key[*Service], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Service, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid()
		}
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Service, error) {
			service, err := construct(r)
			if err != nil {
				return nil, err
			}
			if err := service.Validate(); err != nil {
				return nil, err
			}
			return service, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		service, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("outbound webhooks", func(ctx context.Context) error {
			err := service.Close(ctx)
			<-service.Done()
			return errors.Join(err, service.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, service.Close(ctx))
		}
		return nil
	}}
}
