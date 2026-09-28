package attachments

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module owns the manager and requires providers owning its borrowed store,
// disks, image engine and locale catalog. Workers invoking it must stop first.
func Module(name foundation.ProviderID, key foundation.Key[*Manager], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Manager, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid()
		}
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Manager, error) {
			m, err := construct(r)
			if err != nil {
				return nil, err
			}
			if err := m.Validate(); err != nil {
				return nil, err
			}
			return m, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		m, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("attachments", func(ctx context.Context) error {
			err := m.Close(ctx)
			<-m.Done()
			return errors.Join(err, m.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, m.Close(ctx))
		}
		return nil
	}}
}
