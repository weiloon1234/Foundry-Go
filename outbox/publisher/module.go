package publisher

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module starts one managed publisher after its borrowed database and queue
// providers boot. Foundation drains it before shutting either dependency down.
// The publisher logs through Config.Logger, or the application logger when nil.
func Module(name foundation.ProviderID, key foundation.Key[*Publisher], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Publisher, error)) foundation.Module {
	return KernelModule(name, key, nil, requires, construct)
}

// KernelModule starts the publisher only while one of kernels runs, so a CLI
// command or an HTTP-only replica does not also poll the outbox. Empty kernels
// keep Module's behavior of publishing under every kernel, as does an
// application started without selecting kernels. Running it beside the worker
// or scheduler kernel is the recommended deployment.
func KernelModule(name foundation.ProviderID, key foundation.Key[*Publisher], kernels []foundation.KernelKind, requires []foundation.ProviderID, construct func(foundation.Resolver) (*Publisher, error)) foundation.Module {
	kernels = slices.Clone(kernels)
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "outbox module requires a publisher factory")
		}
		return foundation.Factory(r, key, construct)
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		publisher, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if !selected(r, kernels) {
			return nil
		}
		logger := publisher.config.Logger
		if logger == nil {
			logger = r.Logger()
		}
		return r.Go("outbox", func(ctx context.Context) error { return publisher.run(ctx, logger) })
	}}
}

func selected(r *foundation.Runtime, kernels []foundation.KernelKind) bool {
	running := r.SelectedKernels()
	if len(kernels) == 0 || len(running) == 0 {
		return true
	}
	for _, kind := range running {
		if slices.Contains(kernels, kind) {
			return true
		}
	}
	return false
}
