package local

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module prepares an adapter without I/O and opens its root during boot. Disk
// modules must require this provider so their readers drain before root cleanup.
func Module(name foundation.ProviderID, key foundation.Key[*Backend], config Config) foundation.Module {
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(foundation.Resolver) (*Backend, error) { return Prepare(config) })
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		backend, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("storage-root", func(context.Context) error { return backend.Close() }); err != nil {
			return errors.Join(err, backend.Close())
		}
		return backend.Start(ctx)
	}}
}
