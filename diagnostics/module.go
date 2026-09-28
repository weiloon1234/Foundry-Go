package diagnostics

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
)

// Module borrows readiness dependencies and the application's recorder. Include
// the readiness provider in requires. It starts no goroutines or HTTP servers;
// resolve the returned service in the ordinary HTTP handler factory.
func Module(name foundation.ProviderID, key foundation.Key[*Runtime], probes foundation.Key[*health.Registry], config Config, requires []foundation.ProviderID) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(registrar *foundation.Registrar) error {
		if err := config.Validate(); err != nil {
			return err
		}
		return foundation.Factory(registrar, key, func(resolver foundation.Resolver) (*Runtime, error) {
			registry, err := foundation.Resolve(resolver, probes)
			if err != nil {
				return nil, err
			}
			return prepare(config, registry)
		})
	}, OnBoot: func(_ context.Context, app *foundation.Runtime) error {
		runtime, err := foundation.Resolve(app.Services(), key)
		if err != nil {
			return err
		}
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		if runtime.state != nil {
			return fault.New(fault.Conflict, "diagnostics already belongs to an application")
		}
		runtime.state, runtime.recorder = app.State, app.Observability()
		return nil
	}}
}
