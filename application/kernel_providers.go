package application

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/websocket"
	"slices"
)

const JobDeclarationsProvider foundation.ProviderID = "foundry.application.jobs"
const EventProvider foundation.ProviderID = "foundry.application.events"
const EventDeclarationsProvider foundation.ProviderID = "foundry.application.listeners"
const WorkerProvider foundation.ProviderID = "foundry.application.worker"
const SchedulerProvider foundation.ProviderID = "foundry.application.scheduler"
const RealtimeProvider foundation.ProviderID = "foundry.application.realtime"

var EventKey = foundation.NewKey[*events.Bus](string(EventProvider))
var SchedulerKey = foundation.NewKey[*schedule.Scheduler](string(SchedulerProvider))
var RealtimeKey = foundation.NewKey[*websocket.Hub](string(RealtimeProvider))

func registerKernelDeclarations(builder *foundation.Builder, plan *infrastructure.Plan, s Settings, source clock.Clock, jobDeclarations []JobDeclaration, eventDeclarations []EventDeclaration, schedules []Schedules, realtime Realtime) error {
	if len(jobDeclarations) > 4096 || len(eventDeclarations) > 4096 || len(schedules) > 128 {
		return fault.New(fault.Invalid, "too many application declarations")
	}
	if len(jobDeclarations) > 0 {
		builder.Register(foundation.Module{Name: JobDeclarationsProvider, Requires: []foundation.ProviderID{Provider}, OnRegister: func(r *foundation.Registrar) error {
			for _, d := range jobDeclarations {
				name := d.connection
				if name == "" {
					name = s.Services.Jobs.Default
				}
				if _, ok := s.Services.Jobs.Connections[name]; !ok {
					return fault.New(fault.Missing, "job declaration connection is not configured")
				}
				if d.install == nil {
					return fault.New(fault.Invalid, "invalid job declaration")
				}
				if err := d.install(r, name); err != nil {
					return err
				}
			}
			return nil
		}})
	}
	if len(eventDeclarations) > 0 && !s.Features.Events.Enabled {
		return fault.New(fault.Invalid, "event declarations require events to be enabled")
	}
	if s.Features.Events.Enabled {
		module := events.Module(EventProvider, EventKey, s.Features.Events.Config)
		module.Requires = []foundation.ProviderID{Provider}
		builder.Register(module)
		builder.Register(foundation.Module{Name: EventDeclarationsProvider, Requires: []foundation.ProviderID{EventProvider}, OnRegister: func(r *foundation.Registrar) error {
			for _, d := range eventDeclarations {
				if d.install == nil {
					return fault.New(fault.Invalid, "invalid event declaration")
				}
				if err := d.install(r); err != nil {
					return err
				}
			}
			return nil
		}})
	}
	if s.Worker.Enabled {
		requires := []foundation.ProviderID{Provider}
		if len(jobDeclarations) > 0 {
			requires = append(requires, JobDeclarationsProvider)
		}
		worker, err := plan.Worker(WorkerProvider, s.Worker.Connection, s.Worker.Config, requires)
		if err != nil {
			return err
		}
		builder.Register(worker)
	}
	if len(schedules) > 0 && !s.Scheduler.Enabled {
		return fault.New(fault.Invalid, "schedule declarations require scheduler to be enabled")
	}
	if s.Scheduler.Enabled {
		if !s.Services.Coordination.Enabled {
			return fault.New(fault.Missing, "scheduler requires configured coordination")
		}
		config := s.Scheduler.runtime(source)
		if err := config.Validate(); err != nil {
			return err
		}
		builder.Register(schedule.Module(SchedulerProvider, SchedulerKey, config, []foundation.ProviderID{Provider, infrastructure.CoordinationProvider}, func(r foundation.Resolver) (*lease.Manager, []schedule.Declaration, error) {
			resources, err := FromResolver(r)
			if err != nil {
				return nil, nil, err
			}
			manager, err := resources.Leases()
			if err != nil {
				return nil, nil, err
			}
			var result []schedule.Declaration
			for _, construct := range schedules {
				items, err := construct(resources)
				if err != nil {
					return nil, nil, err
				}
				result = append(result, items...)
			}
			return manager, result, nil
		}))
	}
	if !s.Realtime.Enabled && realtime != nil {
		return fault.New(fault.Invalid, "realtime declarations require realtime to be enabled")
	}
	if s.Realtime.Enabled {
		if realtime == nil {
			return fault.New(fault.Missing, "realtime requires typed channel declarations")
		}
		name := s.Realtime.Connection
		if name == "" {
			name = s.Services.Realtime.Default
		}
		if _, ok := s.Services.Realtime.Connections[name]; !ok {
			return fault.New(fault.Missing, "realtime connection is not configured")
		}
		if err := s.Realtime.Config.Validate(); err != nil {
			return err
		}
		// Capture domain declarations once before creating the existing module. The
		// registrar factory keeps actual construction pure and inside graph validation.
		declarationKey := foundation.NewKey[RealtimeDeclarations](string(RealtimeProvider) + ".declarations")
		builder.Register(foundation.Module{Name: foundation.ProviderID(declarationKey.Name()), Requires: []foundation.ProviderID{Provider}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, declarationKey, func(r foundation.Resolver) (RealtimeDeclarations, error) {
				resources, err := FromResolver(r)
				if err != nil {
					return RealtimeDeclarations{}, err
				}
				d, err := realtime(resources)
				d.Channels = slices.Clone(d.Channels)
				d.Middleware = slices.Clone(d.Middleware)
				return d, err
			})
		}})
		// Middleware comes from declarations, so apply it to the hub upgrade handler
		// through the existing module's configurable handler boundary below.
		module := websocket.DeclaredModule(RealtimeProvider, RealtimeKey, websocket.ServerConfig{HTTP: s.Realtime.HTTP, Path: s.Realtime.Path}, []foundation.ProviderID{Provider, infrastructure.RealtimeProvider(name)}, func(r foundation.Resolver) (*websocket.Hub, []foundryhttp.Middleware, error) {
			d, err := foundation.Resolve(r, declarationKey)
			if err != nil {
				return nil, nil, err
			}
			registry, err := websocket.NewRegistry(d.Channels...)
			if err != nil {
				return nil, nil, err
			}
			connection, err := foundation.Resolve(r, infrastructure.RealtimeKey(name))
			if err != nil {
				return nil, nil, err
			}
			hub, err := connection.NewHub(registry, d.Authentication, s.Realtime.Config)
			return hub, d.Middleware, err
		})
		builder.Register(module)
	}
	return nil
}
func (s Services) Events() (*events.Bus, error)            { return Resolve(s, EventKey) }
func (s Services) Scheduler() (*schedule.Scheduler, error) { return Resolve(s, SchedulerKey) }
func (s Services) RealtimeHub() (*websocket.Hub, error)    { return Resolve(s, RealtimeKey) }
func (a *App) RealtimeReady(ctx context.Context) (string, error) {
	if a == nil {
		return "", fault.New(fault.Missing, "realtime is not configured")
	}
	hub, err := a.Resources().RealtimeHub()
	if err != nil {
		return "", err
	}
	return hub.Ready(ctx)
}
