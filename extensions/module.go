package extensions

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module owns its Store and requires providers owning its borrowed database.
// Feature managers using the store must in turn depend on this module.
func Module(name foundation.ProviderID, key foundation.Key[*Store], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Store, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid("model extension module requires a constructor")
		}
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Store, error) {
			s, err := construct(r)
			if err != nil {
				return nil, err
			}
			if err := s.Validate(); err != nil {
				return nil, err
			}
			return s, nil
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		s, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("model-extensions", func(ctx context.Context) error {
			err := s.Close(ctx)
			<-s.Done()
			return errors.Join(err, s.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, s.Close(ctx))
		}
		return nil
	}}
}
