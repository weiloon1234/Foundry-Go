package application

import (
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// JobDeclaration is only an assembly envelope; payload contracts remain in the
// captured Definition and RegisterJob's compiler-checked contribution boundary.
type JobDeclaration struct {
	connection jobs.ConnectionName
	install    func(*foundation.Registrar, jobs.ConnectionName) error
}

// Job declares a typed job with a handler constructor and default handler options.
func Job[P any](definition jobs.Definition[P], construct func(Services) (jobs.Handler[P], error)) JobDeclaration {
	return JobWith(definition, func(services Services) (jobs.Handler[P], jobs.HandlerOptions[P], error) {
		if construct == nil {
			return nil, jobs.HandlerOptions[P]{}, safeerror.NewFault(fault.Invalid, "job requires a typed handler constructor")
		}
		handler, err := construct(services)
		return handler, jobs.HandlerOptions[P]{}, err
	})
}

// JobWith binds typed middleware and admission using the same configured
// registration as Job. Use Failed middleware for explicit domain diagnostics;
// worker failure metadata is logged automatically by the configured worker.
func JobWith[P any](definition jobs.Definition[P], construct func(Services) (jobs.Handler[P], jobs.HandlerOptions[P], error)) JobDeclaration {
	return JobDeclaration{install: func(r *foundation.Registrar, connection jobs.ConnectionName) error {
		if construct == nil {
			return safeerror.NewFault(fault.Invalid, "job requires a typed handler constructor")
		}
		return jobs.RegisterJob(r, infrastructure.JobDispatcherKey(connection), definition, func(resolver foundation.Resolver) (jobs.Declaration, error) {
			services, err := FromResolver(resolver)
			if err != nil {
				return jobs.Declaration{}, err
			}
			handler, options, err := construct(services)
			if err != nil {
				return jobs.Declaration{}, err
			}
			return definition.DeclareWith(handler, options)
		})
	}}
}

// On selects a named backend for registration; zero selects the configured default.
func (d JobDeclaration) On(connection jobs.ConnectionName) JobDeclaration {
	d.connection = connection
	return d
}

// Jobs adds job declarations to the builder.
func (b *Builder) Jobs(declarations ...JobDeclaration) *Builder {
	b.mutate(func() { b.state.jobs = append(b.state.jobs, declarations...) })
	return b
}

// EventDeclaration holds an event registration and optional outbox declaration.
type EventDeclaration struct {
	install func(*foundation.Registrar) error
	outbox  *eventOutboxDeclaration
}

// Listen declares a typed event listener whose handler is constructed from application services.
func Listen[E any](topic events.Topic[E], name events.ListenerID, construct func(Services) (events.Handler[E], error)) EventDeclaration {
	return EventDeclaration{install: func(r *foundation.Registrar) error {
		if construct == nil {
			return safeerror.NewFault(fault.Invalid, "event listener requires a typed handler constructor")
		}
		return events.RegisterListener(r, EventKey, topic, name, func(resolver foundation.Resolver) (events.Handler[E], error) {
			services, err := FromResolver(resolver)
			if err != nil {
				return nil, err
			}
			return construct(services)
		})
	}}
}

// ListenQueued registers a listener that runs as a job instead of inline.
// It requires features.events.queued_listeners.
func ListenQueued[E any](topic events.Topic[E], name events.ListenerID, construct func(Services) (events.Handler[E], error)) EventDeclaration {
	return EventDeclaration{install: func(r *foundation.Registrar) error {
		if construct == nil {
			return safeerror.NewFault(fault.Invalid, "event listener requires a typed handler constructor")
		}
		return events.RegisterQueuedListener(r, EventKey, topic, name, func(resolver foundation.Resolver) (events.Handler[E], error) {
			services, err := FromResolver(resolver)
			if err != nil {
				return nil, err
			}
			return construct(services)
		})
	}}
}

// Subscribe registers every listener of one subscriber type constructed from
// the application's services. id names the subscriber uniquely.
func Subscribe(id string, construct func(Services) (events.Subscriber, error)) EventDeclaration {
	return EventDeclaration{install: func(r *foundation.Registrar) error {
		if construct == nil {
			return safeerror.NewFault(fault.Invalid, "event subscriber requires a constructor")
		}
		return events.RegisterSubscriber(r, EventKey, id, func(resolver foundation.Resolver) (events.Subscriber, error) {
			services, err := FromResolver(resolver)
			if err != nil {
				return nil, err
			}
			return construct(services)
		})
	}}
}

// Topic declares an event topic for registration.
func Topic[E any](topic events.Topic[E]) EventDeclaration {
	return EventDeclaration{install: func(r *foundation.Registrar) error { return events.RegisterTopic(r, EventKey, topic) }}
}

// Events adds event declarations to the builder.
func (b *Builder) Events(declarations ...EventDeclaration) *Builder {
	b.mutate(func() { b.state.events = append(b.state.events, declarations...) })
	return b
}

// Schedules constructs schedule declarations from application services.
type Schedules func(Services) ([]schedule.Declaration, error)

// Schedules adds a schedule constructor, recording an invalid fault if it is nil.
func (b *Builder) Schedules(construct Schedules) *Builder {
	b.mutate(func() {
		if construct == nil {
			b.state.err = safeerror.NewFault(fault.Invalid, "nil schedule declarations")
			return
		}
		b.state.schedules = append(b.state.schedules, construct)
	})
	return b
}

// RealtimeDeclarations contains domain channels, typed authentication and upgrade
// middleware. Tickets lists the token guards whose single-use handshake tickets
// the hub redeems (see websocket.WithTickets); each guard's credential source
// must also be declared by Authentication. The framework owns the selected hub,
// listener and shutdown.
type RealtimeDeclarations struct {
	Channels       []websocket.Registration
	Authentication *http.Authentication
	Middleware     []http.Middleware
	Tickets        []websocket.TicketRedeemer
}

// hubOptions turns declared collaborators into hub construction options.
func (d RealtimeDeclarations) hubOptions() []websocket.Option {
	if len(d.Tickets) == 0 {
		return nil
	}
	return []websocket.Option{websocket.WithTickets(d.Tickets...)}
}

// Realtime constructs realtime declarations from application services.
type Realtime func(Services) (RealtimeDeclarations, error)

// Realtime sets the realtime constructor, recording an invalid fault for a nil or duplicate
// constructor.
func (b *Builder) Realtime(construct Realtime) *Builder {
	b.mutate(func() {
		if construct == nil || b.state.realtime != nil {
			b.state.err = safeerror.NewFault(fault.Invalid, "application requires exactly one realtime declaration constructor")
			return
		}
		b.state.realtime = construct
	})
	return b
}
