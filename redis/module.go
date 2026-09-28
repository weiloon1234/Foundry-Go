package redis

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module prepares a distinct client for each application and starts it at boot.
// Dependents resolve the typed key during construction; cleanup is installed
// before the connectivity check and participates in reverse-order shutdown.
func Module(name foundation.ProviderID, key foundation.Key[*Client], config Config) foundation.Module {
	config = config.snapshot()
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(foundation.Resolver) (*Client, error) { return Prepare(config) })
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		client, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err = r.OnShutdown("connections", func(ctx context.Context) error {
			err := client.Close(ctx)
			<-client.Done()
			return errors.Join(err, client.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, client.Close(ctx))
		}
		return client.Start(ctx)
	}}
}
