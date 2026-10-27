package httpclient

import (
	"context"
	"net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module binds one named upstream to its typed service key. requires identifies
// providers owning a borrowed transport so all operations drain before teardown.
func Module(name foundation.ProviderID, key foundation.Key[*Client], config Config, requires []foundation.ProviderID, transport func(foundation.Resolver) (http.RoundTripper, error)) foundation.Module {
	// Capture mutable header values and policy slices now, before deferred
	// provider construction; later caller mutation cannot change the module.
	configErr := config.Validate()
	if configErr == nil {
		config.Headers, configErr = copyHeaders(config.Headers, config.HeaderBytes, true)
		config.Destination = config.Destination.snapshot()
		config.Retry = config.Retry.snapshot()
	}
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if configErr != nil {
			return configErr
		}
		return foundation.Factory(r, key, func(resolver foundation.Resolver) (*Client, error) {
			var adapter http.RoundTripper
			if transport != nil {
				var err error
				adapter, err = transport(resolver)
				if err != nil {
					return nil, err
				}
			}
			return New(config, adapter)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		client, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("httpclient", func(ctx context.Context) error {
			err := client.Close(ctx)
			<-client.Done()
			return safeerror.Join("operation failed", err, client.Close(context.Background()))
		}); err != nil {
			return safeerror.Join("operation failed", err, client.Close(ctx))
		}
		return nil
	}}
}
