package storage

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module builds one disk per application. requires names providers owning its
// borrowed backend, so disk operations/readers drain before the backend closes.
// The backend factory must construct without I/O; adapter modules start at boot.
func Module(name foundation.ProviderID, key foundation.Key[*Disk], declaration Declaration, config Config, requires []foundation.ProviderID, backend func(foundation.Resolver) (Backend, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Disk, error) {
			if backend == nil {
				return NewDisk(declaration.ID(), nil, config)
			}
			adapter, err := backend(r)
			if err != nil {
				return nil, err
			}
			return declaration.Bind(adapter, config)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		disk, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("storage", func(ctx context.Context) error {
			err := disk.Close(ctx)
			<-disk.Done()
			return safeerror.Join("operation failed", err, disk.Close(context.Background()))
		}); err != nil {
			return safeerror.Join("operation failed", err, disk.Close(ctx))
		}
		return nil
	}}
}
