package events

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

type outboxRegistration struct{ producer *Outbox }

// RegisterOutbox binds a typed producer through ordinary application assembly.
// A durable destination may be registered only once per application, even when
// different public service keys or buses are supplied. Duplicate detection and
// constructor lifetime remain owned by foundation's existing service graph.
func RegisterOutbox(r *foundation.Registrar, key foundation.Key[*Outbox], busKey foundation.Key[*Bus], destination outbox.Destination) error {
	if err := destination.Validate(); err != nil {
		return err
	}
	registration := foundation.NewKey[outboxRegistration](fmt.Sprintf("foundry.events.outbox.%q", destination))
	if err := foundation.Factory(r, registration, func(resolver foundation.Resolver) (outboxRegistration, error) {
		bus, err := managedBus(resolver, busKey)
		if err != nil {
			return outboxRegistration{}, err
		}
		producer, err := PrepareOutbox(destination, bus)
		return outboxRegistration{producer: producer}, err
	}); err != nil {
		return err
	}
	return foundation.Factory(r, key, func(resolver foundation.Resolver) (*Outbox, error) {
		bound, err := foundation.Resolve(resolver, registration)
		return bound.producer, err
	})
}
