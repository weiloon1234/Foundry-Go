package health

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module owns readiness callbacks through ordinary application shutdown. List
// probed dependency providers in requires so this registry drains before those
// resources close. The factory resolves dependencies but must perform no I/O.
// This module starts no listener and does not turn dependency failure into a
// liveness failure; the application's protected diagnostics transport owns that.
func Module(name foundation.ProviderID, key foundation.Key[*Registry], config Config, requires []foundation.ProviderID, construct func(foundation.Resolver) ([]Probe, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "readiness module requires a probe factory")
		}
		if err := config.Validate(); err != nil {
			return err
		}
		return foundation.Factory(r, key, func(resolver foundation.Resolver) (*Registry, error) {
			probes, err := construct(resolver)
			if err != nil {
				return nil, err
			}
			return NewRegistry(config, probes...)
		})
	}, OnBoot: func(ctx context.Context, runtime *foundation.Runtime) error {
		registry, err := foundation.Resolve(runtime.Services(), key)
		if err != nil {
			return err
		}
		if err := runtime.OnShutdown("probes", func(ctx context.Context) error {
			err := registry.Close(ctx)
			<-registry.Done()
			return errors.Join(err, registry.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, registry.Close(ctx))
		}
		return nil
	}}
}
