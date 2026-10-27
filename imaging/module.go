package imaging

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module constructs the configured engine and drains its actual operations at
// shutdown. Applications borrow the resolved engine; the module closes it.
func Module(name foundation.ProviderID, key foundation.Key[*Engine], config Config) foundation.Module {
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		if err := config.Validate(); err != nil {
			return err
		}
		return foundation.Factory(r, key, func(foundation.Resolver) (*Engine, error) { return New(config) })
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		engine, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("imaging", func(ctx context.Context) error {
			err := engine.Close(ctx)
			<-engine.Done()
			return safeerror.Join("operation failed", err, engine.Close(context.Background()))
		}); err != nil {
			return safeerror.Join("operation failed", err, engine.Close(ctx))
		}
		if config.Backend == AutoBackend && engine.NativeError() != nil {
			r.Logger().WarnContext(ctx, "native image features are unavailable; portable imaging remains available", "reason", engine.NativeError())
		}
		return nil
	}}
}
