package httpclient

import (
	"context"
	"net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Module binds one named upstream to its typed service key. requires identifies
// providers owning a borrowed transport so all operations drain before teardown.
// Inside an application with outbound capture, every attempt of the client is
// captured unless a deny-list rule excludes it.
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
			// Only application assembly contributes a recorder, at most one;
			// an empty collection is the ordinary case elsewhere.
			sinks, err := foundation.Contributions(resolver, httpcapture.OutboundSinks)
			if err != nil {
				return nil, err
			}
			if len(sinks) > 1 {
				return nil, invalid()
			}
			var lane *httpcapture.ClientLane
			if len(sinks) == 1 {
				lane = sinks[0].Lane(string(config.Name))
			}
			return newClient(config, adapter, lane)
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
