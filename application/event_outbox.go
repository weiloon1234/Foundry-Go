package application

import (
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

type eventOutboxDeclaration struct {
	key         foundation.Key[*events.Outbox]
	destination outbox.Destination
}

// EventOutbox declares a producer for Topic.Enqueue under key, on the configured
// event bus. features.outbox.events must map destination to a durable job
// connection. Build registers its QueuedOutbox job and publication route; the
// application's ordinary worker delivers it. Events and outbox must be enabled.
func EventOutbox(key foundation.Key[*events.Outbox], destination outbox.Destination) EventDeclaration {
	return EventDeclaration{
		outbox: &eventOutboxDeclaration{key: key, destination: destination},
		install: func(r *foundation.Registrar) error {
			return events.RegisterOutbox(r, key, EventKey, destination)
		},
	}
}

const EventOutboxProvider foundation.ProviderID = "foundry.application.events.outbox"

var eventOutboxRoutesKey = foundation.NewKey[[]publisher.Route](string(EventOutboxProvider) + ".routes")

type eventOutboxGroup struct {
	connection jobs.ConnectionName
	policy     jobs.Policy
	producers  []eventOutboxDeclaration
}

func (g eventOutboxGroup) key() foundation.Key[*events.QueuedOutbox] {
	return foundation.NewKey[*events.QueuedOutbox](fmt.Sprintf("%s.%q", EventOutboxProvider, g.connection))
}

// Validate the complete declaration/configuration join before constructing any
// resources. A destination has one producer and one configured delivery owner.
func prepareEventOutboxes(s Settings, declarations []EventDeclaration) ([]eventOutboxGroup, error) {
	configured := s.Features.Outbox.Events
	declared := make(map[outbox.Destination]eventOutboxDeclaration)
	keys := make(map[foundation.Key[*events.Outbox]]bool)
	for _, declaration := range declarations {
		if declaration.outbox == nil {
			continue
		}
		d := *declaration.outbox
		if err := d.destination.Validate(); err != nil {
			return nil, err
		}
		if d.key.Name() == "" {
			return nil, safeerror.NewFault(fault.Invalid, "event outbox requires a producer key")
		}
		if _, exists := declared[d.destination]; exists || keys[d.key] {
			return nil, safeerror.NewFault(fault.Duplicate, "event outbox destination or producer key is declared twice")
		}
		declared[d.destination], keys[d.key] = d, true
	}
	if len(configured)+len(declared) == 0 {
		return nil, nil
	}
	if !s.Features.Events.Enabled || !s.Features.Outbox.Enabled {
		return nil, safeerror.NewFault(fault.Invalid, "event outboxes require features.events and features.outbox to be enabled")
	}
	if len(configured) > 128 || len(declared) > 128 {
		return nil, safeerror.NewFault(fault.Invalid, "too many configured event outbox destinations")
	}
	for destination := range declared {
		if _, exists := configured[destination]; !exists {
			return nil, safeerror.NewFault(fault.Missing, "event outbox destination has no features.outbox.events connection")
		}
	}
	destinations := make([]outbox.Destination, 0, len(configured))
	for destination := range configured {
		destinations = append(destinations, destination)
	}
	slices.Sort(destinations)
	groups := make(map[jobs.ConnectionName]*eventOutboxGroup)
	for _, destination := range destinations {
		if err := destination.Validate(); err != nil {
			return nil, err
		}
		d, exists := declared[destination]
		if !exists {
			return nil, safeerror.NewFault(fault.Missing, "features.outbox.events destination has no EventOutbox declaration")
		}
		name := configured[destination]
		if name == "" {
			name = s.Services.Jobs.Default
		}
		connection, exists := s.Services.Jobs.Connections[name]
		if !exists {
			return nil, safeerror.NewFault(fault.Missing, "event outbox job connection is not configured")
		}
		if connection.Driver != infrastructure.RedisJobs {
			return nil, safeerror.NewFault(fault.Invalid, "event outbox requires a durable Redis job connection")
		}
		group := groups[name]
		if group == nil {
			policy := jobs.DefaultPolicy(connection.DefaultQueue)
			if err := policy.Validate(); err != nil {
				return nil, err
			}
			group = &eventOutboxGroup{connection: name, policy: policy}
			groups[name] = group
		}
		group.producers = append(group.producers, d)
	}
	names := make([]jobs.ConnectionName, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	slices.Sort(names)
	result := make([]eventOutboxGroup, 0, len(names))
	for _, name := range names {
		result = append(result, *groups[name])
	}
	return result, nil
}

func registerEventOutboxes(builder *foundation.Builder, groups []eventOutboxGroup) {
	if len(groups) == 0 {
		return
	}
	requires := []foundation.ProviderID{EventDeclarationsProvider}
	for _, group := range groups {
		requires = append(requires, infrastructure.JobProvider(group.connection))
	}
	builder.Register(foundation.Module{Name: EventOutboxProvider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		for _, group := range groups {
			key := group.key()
			if err := foundation.Factory(r, key, func(resolver foundation.Resolver) (*events.QueuedOutbox, error) {
				producers := make([]*events.Outbox, 0, len(group.producers))
				for _, d := range group.producers {
					producer, err := foundation.Resolve(resolver, d.key)
					if err != nil {
						return nil, err
					}
					producers = append(producers, producer)
				}
				return events.NewQueuedOutbox(group.policy, producers...)
			}); err != nil {
				return err
			}
			if err := jobs.RegisterJob(r, infrastructure.JobDispatcherKey(group.connection), events.QueuedOutboxDefinition(group.policy), func(resolver foundation.Resolver) (jobs.Declaration, error) {
				queue, err := foundation.Resolve(resolver, key)
				if err != nil {
					return jobs.Declaration{}, err
				}
				return queue.Declaration()
			}); err != nil {
				return err
			}
		}
		return foundation.Factory(r, eventOutboxRoutesKey, func(resolver foundation.Resolver) ([]publisher.Route, error) {
			var routes []publisher.Route
			for _, group := range groups {
				queue, err := foundation.Resolve(resolver, group.key())
				if err != nil {
					return nil, err
				}
				dispatcher, err := foundation.Resolve(resolver, infrastructure.JobDispatcherKey(group.connection))
				if err != nil {
					return nil, err
				}
				for _, d := range group.producers {
					route, err := queue.PublicationRoute(d.destination, dispatcher)
					if err != nil {
						return nil, err
					}
					routes = append(routes, route)
				}
			}
			return routes, nil
		})
	}})
}
