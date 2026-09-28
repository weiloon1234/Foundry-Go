package s3

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module prepares an adapter without I/O and loads its SDK configuration during boot. Disk
// modules must require this provider so their readers drain before transport cleanup.
func Module(name foundation.ProviderID, key foundation.Key[*Backend], config Config) foundation.Module {
	return module(name, key, nil, func(foundation.Resolver) (Config, error) { return config, nil })
}

// CredentialsModule resolves a shared framework credential provider during pure
// construction. Its owner must appear in requires for reverse-order cleanup.
func CredentialsModule(name foundation.ProviderID, key foundation.Key[*Backend], config Config, requires []foundation.ProviderID, provider func(foundation.Resolver) (credentials.Provider, error)) foundation.Module {
	return module(name, key, requires, func(r foundation.Resolver) (Config, error) {
		if provider == nil {
			return Config{}, fault.New(fault.Invalid, "cloud credential resolver is missing")
		}
		value, err := provider(r)
		return config.WithCredentials(value), err
	})
}
func module(name foundation.ProviderID, key foundation.Key[*Backend], requires []foundation.ProviderID, configure func(foundation.Resolver) (Config, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: append([]foundation.ProviderID(nil), requires...), OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Backend, error) {
			config, err := configure(r)
			if err != nil {
				return nil, err
			}
			return Prepare(config)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		backend, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("storage-transport", func(context.Context) error { return backend.Close() }); err != nil {
			return errors.Join(err, backend.Close())
		}
		return backend.Start(ctx)
	}}
}
