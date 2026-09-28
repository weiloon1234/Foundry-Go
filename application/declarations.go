package application

import (
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
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

func Job[P any](definition jobs.Definition[P], construct func(Services) (jobs.Handler[P], error)) JobDeclaration {
	return JobDeclaration{install: func(r *foundation.Registrar, connection jobs.ConnectionName) error {
		if construct == nil {
			return fault.New(fault.Invalid, "job requires a typed handler constructor")
		}
		return jobs.RegisterJob(r, infrastructure.JobDispatcherKey(connection), definition, func(resolver foundation.Resolver) (jobs.Declaration, error) {
			services, err := FromResolver(resolver)
			if err != nil {
				return jobs.Declaration{}, err
			}
			handler, err := construct(services)
			if err != nil {
				return jobs.Declaration{}, err
			}
			return definition.Declare(handler)
		})
	}}
}

// On selects a named backend for registration; zero selects the configured default.
func (d JobDeclaration) On(connection jobs.ConnectionName) JobDeclaration {
	d.connection = connection
	return d
}
func (b *Builder) Jobs(declarations ...JobDeclaration) *Builder {
	b.mutate(func() { b.state.jobs = append(b.state.jobs, declarations...) })
	return b
}

type EventDeclaration struct {
	install func(*foundation.Registrar) error
}

func Listen[E any](topic events.Topic[E], name events.ListenerID, construct func(Services) (events.Handler[E], error)) EventDeclaration {
	return EventDeclaration{install: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "event listener requires a typed handler constructor")
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
func Topic[E any](topic events.Topic[E]) EventDeclaration {
	return EventDeclaration{install: func(r *foundation.Registrar) error { return events.RegisterTopic(r, EventKey, topic) }}
}
func (b *Builder) Events(declarations ...EventDeclaration) *Builder {
	b.mutate(func() { b.state.events = append(b.state.events, declarations...) })
	return b
}

type Schedules func(Services) ([]schedule.Declaration, error)

func (b *Builder) Schedules(construct Schedules) *Builder {
	b.mutate(func() {
		if construct == nil {
			b.state.err = fault.New(fault.Invalid, "nil schedule declarations")
			return
		}
		b.state.schedules = append(b.state.schedules, construct)
	})
	return b
}

// RealtimeDeclarations contains domain channels, typed authentication and upgrade
// middleware. The framework owns the selected hub, listener and shutdown.
type RealtimeDeclarations struct {
	Channels       []websocket.Registration
	Authentication *http.Authentication
	Middleware     []http.Middleware
}
type Realtime func(Services) (RealtimeDeclarations, error)

func (b *Builder) Realtime(construct Realtime) *Builder {
	b.mutate(func() {
		if construct == nil || b.state.realtime != nil {
			b.state.err = fault.New(fault.Invalid, "application requires exactly one realtime declaration constructor")
			return
		}
		b.state.realtime = construct
	})
	return b
}
