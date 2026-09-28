package cli

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module installs the parsed invocation as the application's CLI kernel. Parse
// it using registry before building/starting the application, then reuse that
// registry here. The selected handler receives the same typed services as every
// other kernel, after providers boot, and normal shutdown follows its completion.
func Module(name foundation.ProviderID, key foundation.Key[*Registry], registry *Registry, invocation Invocation, streams Streams, requires ...foundation.ProviderID) foundation.Module {
	return foundation.Module{Name: name, Requires: append([]foundation.ProviderID(nil), requires...), OnRegister: func(r *foundation.Registrar) error {
		if registry == nil || invocation.state == nil || invocation.state.registry != registry {
			return fault.New(fault.Invalid, "CLI module requires an invocation parsed by its registry")
		}
		if err := streams.Validate(); err != nil {
			return err
		}
		if err := foundation.Provide(r, key, registry); err != nil {
			return err
		}
		return r.Kernel(foundation.CLI, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
			return foundation.KernelFunc(func(ctx context.Context) error { return invocation.Run(ctx, runtime.Services(), streams) }), nil
		})
	}}
}
