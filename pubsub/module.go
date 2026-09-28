package pubsub

import (
	"context"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module owns one broker per application. Requires identifies providers owning
// its borrowed backend, keeping adapters alive until subscriptions have drained.
func Module(name foundation.ProviderID, key foundation.Key[*Broker], config Config, requires []foundation.ProviderID, backend func(foundation.Resolver) (Backend, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(r foundation.Resolver) (*Broker, error) {
			if backend == nil {
				return NewBroker(nil, config)
			}
			adapter, err := backend(r)
			if err != nil {
				return nil, err
			}
			return NewBroker(adapter, config)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		broker, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("pubsub", func(ctx context.Context) error {
			err := broker.Close(ctx)
			<-broker.Done()
			return errors.Join(err, broker.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, broker.Close(ctx))
		}
		return nil
	}}
}
