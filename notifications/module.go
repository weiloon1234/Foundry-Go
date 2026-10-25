package notifications

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module owns the manager. Requires must name providers owning all borrowed
// resources, so active notifications drain before database/transports close.
func Module(name foundation.ProviderID, key foundation.Key[*Manager], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Manager, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid()
		}
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Manager, error) {
			manager, err := construct(r)
			if err != nil {
				return nil, err
			}
			if manager == nil || manager.done == nil {
				return nil, invalid()
			}
			return manager, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		manager, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("notifications", func(ctx context.Context) error {
			err := manager.Close(ctx)
			<-manager.Done()
			return safeerror.Join("operation failed", err, manager.Close(context.Background()))
		}); err != nil {
			return safeerror.Join("operation failed", err, manager.Close(ctx))
		}
		return nil
	}}
}
