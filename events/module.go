package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module prepares one bus per application, binds all provider contributions
// before boot, and retains listener work until shutdown has actually completed.
func Module(name foundation.ProviderID, key foundation.Key[*Bus], config Config) foundation.Module {
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, key, func(foundation.Resolver) (*Bus, error) {
			bus, err := prepare(config)
			if err != nil {
				return nil, err
			}
			bus.managed = true
			return bus, nil
		}); err != nil {
			return err
		}
		binding := foundation.NewKey[eventBinding](fmt.Sprintf("foundry.events.binding.%q", key.Name()))
		return foundation.Factory(r, binding, func(resolver foundation.Resolver) (eventBinding, error) {
			bus, err := foundation.Resolve(resolver, key)
			if err != nil {
				return eventBinding{}, err
			}
			contributions, err := foundation.Contributions(resolver, eventContributions(key))
			if err != nil {
				return eventBinding{}, err
			}
			var declarations []Declaration
			for _, contribution := range contributions {
				if contribution.bus == bus {
					declarations = append(declarations, contribution.declarations...)
				}
			}
			return eventBinding{}, bus.bind(declarations, true)
		})
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		bus, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("listeners", func(ctx context.Context) error {
			err := bus.Close(ctx)
			<-bus.Done()
			return errors.Join(err, bus.Close(context.Background()))
		}); err != nil {
			return errors.Join(err, bus.Close(ctx))
		}
		return bus.start(ctx, true)
	}}
}

type eventBinding struct{}

func eventContributions(key foundation.Key[*Bus]) foundation.Collection[eventContribution] {
	return foundation.NewCollection[eventContribution](fmt.Sprintf("events.declarations.%q", key.Name()))
}

type eventContribution struct {
	bus          *Bus
	declarations []Declaration
}

// RegisterTopic declares an intentionally listener-free topic, such as a schema
// used only for durable enqueue. Listener registration also declares its topic;
// callers do not need both. Registration is explicit and local to the selected bus.
func RegisterTopic[E any](r *foundation.Registrar, busKey foundation.Key[*Bus], topic Topic[E]) error {
	declaration, err := topic.Declare()
	if err != nil {
		return err
	}
	return foundation.ContributeAs[E](r, eventContributions(busKey), fmt.Sprintf("topic.%q.%d", topic.Name(), topic.Version()), func(resolver foundation.Resolver) (eventContribution, error) {
		bus, err := managedBus(resolver, busKey)
		if err != nil {
			return eventContribution{}, err
		}
		return eventContribution{bus: bus, declarations: []Declaration{declaration}}, nil
	})
}

// RegisterListener constructs a typed handler once through the application's
// existing service graph. Constructors are pure and may resolve their own bus;
// binding is a separate construction step, avoiding a bus/listener cycle.
func RegisterListener[E any](r *foundation.Registrar, busKey foundation.Key[*Bus], topic Topic[E], name ListenerID, construct func(foundation.Resolver) (Handler[E], error)) error {
	if err := topic.Validate(); err != nil {
		return err
	}
	if err := validateListener(name, construct != nil); err != nil {
		return err
	}
	return foundation.ContributeAs[E](r, eventContributions(busKey), fmt.Sprintf("listener.%q.%d.%q", topic.Name(), topic.Version(), name), func(resolver foundation.Resolver) (eventContribution, error) {
		bus, err := managedBus(resolver, busKey)
		if err != nil {
			return eventContribution{}, err
		}
		handler, err := construct(resolver)
		if err != nil {
			return eventContribution{}, err
		}
		declaration, err := topic.Declare(Listen(name, handler))
		if err != nil {
			return eventContribution{}, err
		}
		return eventContribution{bus: bus, declarations: []Declaration{declaration}}, nil
	})
}

// RegisterQueuedListener is RegisterListener for a listener that runs as a job
// through the bus's ListenerQueue (see Listener.Queued).
func RegisterQueuedListener[E any](r *foundation.Registrar, busKey foundation.Key[*Bus], topic Topic[E], name ListenerID, construct func(foundation.Resolver) (Handler[E], error)) error {
	if err := topic.Validate(); err != nil {
		return err
	}
	if err := validateListener(name, construct != nil); err != nil {
		return err
	}
	return foundation.ContributeAs[E](r, eventContributions(busKey), fmt.Sprintf("listener.%q.%d.%q", topic.Name(), topic.Version(), name), func(resolver foundation.Resolver) (eventContribution, error) {
		bus, err := managedBus(resolver, busKey)
		if err != nil {
			return eventContribution{}, err
		}
		handler, err := construct(resolver)
		if err != nil {
			return eventContribution{}, err
		}
		declaration, err := topic.Declare(Listen(name, handler).Queued())
		if err != nil {
			return eventContribution{}, err
		}
		return eventContribution{bus: bus, declarations: []Declaration{declaration}}, nil
	})
}

// Subscriber groups related listeners in one type, typically constructed with
// shared dependencies. Declarations returns one Topic.Declare per event it
// handles; each may mix sync and queued listeners.
type Subscriber interface {
	Declarations() ([]Declaration, error)
}

// Subscribe collects subscribers' declarations for Prepare.
func Subscribe(subscribers ...Subscriber) ([]Declaration, error) {
	var result []Declaration
	for _, subscriber := range subscribers {
		if subscriber == nil {
			return nil, fault.New(fault.Invalid, "nil event subscriber")
		}
		declarations, err := subscriber.Declarations()
		if err != nil {
			return nil, err
		}
		result = append(result, declarations...)
	}
	return result, nil
}

// RegisterSubscriber constructs a subscriber once through the application's
// service graph and contributes all of its listeners to the bus. id names the
// subscriber uniquely within the bus.
func RegisterSubscriber(r *foundation.Registrar, busKey foundation.Key[*Bus], id string, construct func(foundation.Resolver) (Subscriber, error)) error {
	if construct == nil {
		return fault.New(fault.Invalid, "event subscriber requires a constructor")
	}
	return foundation.ContributeAs[Subscriber](r, eventContributions(busKey), fmt.Sprintf("subscriber.%q", id), func(resolver foundation.Resolver) (eventContribution, error) {
		bus, err := managedBus(resolver, busKey)
		if err != nil {
			return eventContribution{}, err
		}
		subscriber, err := construct(resolver)
		if err != nil {
			return eventContribution{}, err
		}
		declarations, err := Subscribe(subscriber)
		if err != nil {
			return eventContribution{}, err
		}
		return eventContribution{bus: bus, declarations: declarations}, nil
	})
}

func managedBus(resolver foundation.Resolver, key foundation.Key[*Bus]) (*Bus, error) {
	bus, err := foundation.Resolve(resolver, key)
	if err != nil {
		return nil, err
	}
	if bus == nil || !bus.managed {
		return nil, fault.New(fault.Invalid, "registered event contributions require an events Module")
	}
	return bus, nil
}
