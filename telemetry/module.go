package telemetry

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// KernelModule registers an engine and its cleanup before starting a tracked
// capture task. Empty kernels select HTTP, Worker and Scheduler; CLI-only and
// Start without selected kernels expose reads but do not run a periodic loop.
// Requires must include every borrowed resource owner. Cleanup waits for Done
// even after its deadline, preserving dependencies under uncooperative callbacks.
func KernelModule(name foundation.ProviderID, key foundation.Key[*Engine], kernels []foundation.KernelKind, requires []foundation.ProviderID, construct func(foundation.Resolver) (*Engine, error)) foundation.Module {
	kernels = slices.Clone(kernels)
	if len(kernels) == 0 {
		kernels = []foundation.KernelKind{foundation.HTTP, foundation.Worker, foundation.Scheduler}
	}
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid("telemetry module requires an engine factory")
		}
		for i, kind := range kernels {
			switch kind {
			case foundation.HTTP, foundation.Worker, foundation.Scheduler, foundation.CLI, foundation.WebSocket:
			default:
				return invalid("unknown telemetry kernel")
			}
			if slices.Contains(kernels[:i], kind) {
				return safeerror.NewFault(fault.Duplicate, "duplicate telemetry kernel")
			}
		}
		return foundation.Factory(r, key, construct)
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		e, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if e == nil || e.done == nil {
			return invalid("telemetry factory returned an uninitialized engine")
		}
		if err := r.OnShutdown("telemetry", func(ctx context.Context) error {
			err := e.Close(ctx)
			<-e.Done()
			return safeerror.Join("telemetry shutdown", err, e.closedResult())
		}); err != nil {
			closeErr := e.Close(context.Background())
			return safeerror.Join("telemetry cleanup registration", err, closeErr)
		}
		for _, kind := range r.SelectedKernels() {
			if slices.Contains(kernels, kind) {
				return r.Go("telemetry", e.Run)
			}
		}
		return nil
	}}
}
