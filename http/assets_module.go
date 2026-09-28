package http

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// AssetsModule prepares a source during Build, opens its directory during Boot
// and closes it after managed kernels/requests exit. Consumers declare the source
// and resolve *Assets while assembling routes; they do not manage os.Root.
func AssetsModule(name foundation.ProviderID, key foundation.Key[*Assets], config AssetsConfig) foundation.Module {
	config = config.snapshot()
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(foundation.Resolver) (*Assets, error) { return prepareAssets(config) })
	}, OnBoot: func(ctx context.Context, runtime *foundation.Runtime) error {
		assets, err := foundation.Resolve(runtime.Services(), key)
		if err != nil {
			return err
		}
		if err := assets.start(ctx); err != nil {
			return err
		}
		if err := runtime.OnShutdown("assets", func(ctx context.Context) error { return assets.Close(ctx) }); err != nil {
			return errors.Join(err, assets.Close(context.WithoutCancel(ctx)))
		}
		return nil
	}}
}
