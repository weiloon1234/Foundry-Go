package datatable

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module retains borrowed providers until query callbacks and exported files
// actually close. Export workers must declare this module as their dependency.
func Module(name foundation.ProviderID, key foundation.Key[*Manager], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Manager, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid("datatable module requires a manager factory")
		}
		return foundation.Factory(r, key, func(resolver foundation.Resolver) (*Manager, error) {
			manager, err := construct(resolver)
			if err != nil {
				return nil, err
			}
			if err := manager.Validate(); err != nil {
				return nil, err
			}
			return manager, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		manager, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("datatables", func(ctx context.Context) error {
			err := manager.Close(ctx)
			<-manager.DoneQueries()
			<-manager.DoneExports()
			return errors.Join(err, manager.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, manager.Close(ctx))
		}
		return nil
	}}
}
