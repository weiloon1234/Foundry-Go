package http

import (
	"context"
	stdhttp "net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module registers one prepared server and the HTTP kernel. The handler factory
// runs once per application during Build and may resolve explicit dependencies;
// it must not start I/O or goroutines. Only App.Run with foundation.HTTP binds
// the listener, after all providers boot. Other kernels can share this assembly.
// The application supplies its logger and retains services until handlers exit.
func Module(name foundation.ProviderID, key foundation.Key[*Server], config ServerConfig, construct func(foundation.Resolver) (stdhttp.Handler, error), options ...ServerOption) foundation.Module {
	options = slices.Clone(options)
	config = config.Snapshot()
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "HTTP module needs a handler factory")
		}
		if err := config.Validate(); err != nil {
			return err
		}
		if err := foundation.Factory(r, key, func(resolver foundation.Resolver) (*Server, error) {
			handler, err := construct(resolver)
			if err != nil {
				return nil, err
			}
			server, err := prepare(handler, config, options...)
			if err != nil {
				return nil, err
			}
			server.managed = true
			return server, nil
		}); err != nil {
			return err
		}
		return r.Kernel(foundation.HTTP, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
			server, err := foundation.Resolve(runtime.Services(), key)
			if err != nil {
				return nil, err
			}
			return foundation.KernelFunc(func(ctx context.Context) error {
				return server.run(ctx, runtime.Logger(), true)
			}), nil
		})
	}}
}
