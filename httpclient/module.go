package httpclient

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module binds one named upstream to its typed service key. requires identifies
// providers owning a borrowed transport so all operations drain before teardown.
func Module(name foundation.ProviderID, key foundation.Key[*Client], config Config, requires []foundation.ProviderID, transport func(foundation.Resolver) (http.RoundTripper, error)) foundation.Module {
	// Capture mutable header values now, before deferred provider construction.
	configErr := config.Validate()
	if configErr == nil {
		config.Headers, configErr = copyHeaders(config.Headers, config.HeaderBytes, true)
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
			return errors.Join(err, client.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, client.Close(ctx))
		}
		return nil
	}}
}
