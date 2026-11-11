package capture

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module registers the store factory and, at boot, its cleanup before any
// work starts. The writer and retention loop run beside the HTTP kernel, where
// captures originate, or after Start without a selected kernel; CLI and
// worker processes resolve the store for reads and the prune command. Cleanup runs after the HTTP kernel has drained and waits
// for Close to finish. Requires must name every borrowed resource owner.
//
// Example: capture.Module("app.webhook-captures", key, requires, construct)
func Module(name foundation.ProviderID, key foundation.Key[*Store], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Store, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid("webhook capture module requires a store factory")
		}
		return foundation.Factory(r, key, construct)
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		s, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if s == nil || s.log == nil {
			return invalid("webhook capture factory returned an uninitialized store")
		}
		if err := r.OnShutdown("webhook-capture", func(ctx context.Context) error {
			err := s.Close(ctx)
			<-s.Done()
			return err
		}); err != nil {
			return safeerror.Join("webhook capture cleanup registration", err, s.Close(context.Background()))
		}
		// Start without a selected kernel may serve HTTP afterwards, as
		// embedded and test assemblies do; CLI and worker processes skip it.
		if kernels := r.SelectedKernels(); len(kernels) == 0 || slices.Contains(kernels, foundation.HTTP) {
			return r.Go("webhook-capture", s.Run)
		}
		return nil
	}}
}
