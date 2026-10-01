package application

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
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
const RealtimePublisherProvider foundation.ProviderID = "foundry.application.realtime.publisher"

var EventKey = foundation.NewKey[*events.Bus](string(EventProvider))
var SchedulerKey = foundation.NewKey[*schedule.Scheduler](string(SchedulerProvider))
var RealtimeKey = foundation.NewKey[*websocket.Hub](string(RealtimeProvider))
var RealtimePublisherKey = foundation.NewKey[*websocket.Publisher](string(RealtimePublisherProvider))
var realtimeDeclarationsKey = foundation.NewKey[RealtimeDeclarations](string(RealtimeProvider) + ".declarations")

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
		if s.Features.Events.QueuedListeners {
			if err := registerListenerQueue(builder, s); err != nil {
				return err
			}
		}
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
	if s.Worker.Archive.Enabled {
		if err := registerJobArchive(builder, s, source); err != nil {
			return err
		}
	}
	if s.Worker.Enabled {
		requires := []foundation.ProviderID{Provider}
		if len(jobDeclarations) > 0 {
			requires = append(requires, JobDeclarationsProvider)
		}
		// The graceful drain must finish inside the application's shutdown budget,
		// which the lame-duck StopDelay consumes first.
		if err := s.Worker.Config.ValidateShutdown(s.ShutdownTimeout - s.StopDelay); err != nil {
			return err
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
	// Housekeeping settings are validated in every process; only a process
	// running the scheduler registers the leader-only schedules.
	housekeeping, err := maintenanceSchedules(s, source)
	if err != nil {
		return err
	}
	if housekeeping != nil {
		schedules = append(schedules, housekeeping)
	}
	if s.Scheduler.Enabled {
		if !s.Services.Coordination.Enabled {
			return fault.New(fault.Missing, "scheduler requires configured coordination")
		}
		config := s.Scheduler.runtime(source)
		if err := config.Validate(); err != nil {
			return err
		}
		if err := config.ValidateShutdown(s.ShutdownTimeout - s.StopDelay); err != nil {
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
	if !s.Realtime.Enabled && !s.Realtime.Publisher && realtime != nil {
		return fault.New(fault.Invalid, "realtime declarations require realtime or its publisher to be enabled")
	}
	if s.Realtime.Enabled || s.Realtime.Publisher {
		if realtime == nil {
			return fault.New(fault.Missing, "realtime requires typed channel declarations")
		}
		if s.Realtime.Enabled && s.Realtime.Publisher {
			return fault.New(fault.Invalid, "a realtime hub already publishes; disable the standalone publisher")
		}
		if s.Realtime.Shared && (!s.Realtime.Enabled || !s.HTTP.Enabled) {
			return fault.New(fault.Invalid, "shared realtime requires realtime and HTTP to be enabled")
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
		builder.Register(foundation.Module{Name: foundation.ProviderID(realtimeDeclarationsKey.Name()), Requires: []foundation.ProviderID{Provider}, OnRegister: func(r *foundation.Registrar) error {
			return foundation.Factory(r, realtimeDeclarationsKey, func(r foundation.Resolver) (RealtimeDeclarations, error) {
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
		requires := []foundation.ProviderID{Provider, infrastructure.RealtimeProvider(name)}
		declared := func(r foundation.Resolver) (RealtimeDeclarations, *websocket.Registry, *websocket.BackendConnection, error) {
			d, err := foundation.Resolve(r, realtimeDeclarationsKey)
			if err != nil {
				return RealtimeDeclarations{}, nil, nil, err
			}
			registry, err := websocket.NewRegistry(d.Channels...)
			if err != nil {
				return RealtimeDeclarations{}, nil, nil, err
			}
			connection, err := foundation.Resolve(r, infrastructure.RealtimeKey(name))
			return d, registry, connection, err
		}
		switch {
		case s.Realtime.Publisher:
			builder.Register(foundation.Module{Name: RealtimePublisherProvider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
				return foundation.Factory(r, RealtimePublisherKey, func(r foundation.Resolver) (*websocket.Publisher, error) {
					_, registry, connection, err := declared(r)
					if err != nil {
						return nil, err
					}
					return connection.NewPublisher(registry, s.Realtime.Config)
				})
			}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
				publisher, err := foundation.Resolve(r.Services(), RealtimePublisherKey)
				if err != nil {
					return err
				}
				// Like the hub modules, keep ownership until the publisher's
				// operations actually exit, so the borrowed Redis client (closed
				// by an earlier-registered cleanup) outlives them.
				return r.OnShutdown("publisher", func(ctx context.Context) error {
					err := publisher.Close(ctx)
					<-publisher.Done()
					return errors.Join(err, publisher.Close(context.Background()))
				})
			}})
		case s.Realtime.Shared:
			// Middleware comes from declarations; the router mounts the upgrade
			// route with it (see realtimeRoutes).
			builder.Register(websocket.SharedModule(RealtimeProvider, RealtimeKey, HTTPKey, requires, func(r foundation.Resolver) (*websocket.Hub, error) {
				d, registry, connection, err := declared(r)
				if err != nil {
					return nil, err
				}
				return connection.NewHub(registry, d.Authentication, s.Realtime.Config, d.hubOptions()...)
			}))
		default:
			// Middleware comes from declarations, so apply it to the hub upgrade handler
			// through the existing module's configurable handler boundary below.
			builder.Register(websocket.DeclaredModule(RealtimeProvider, RealtimeKey, websocket.ServerConfig{HTTP: s.Realtime.HTTP, Path: s.Realtime.Path}, requires, func(r foundation.Resolver) (*websocket.Hub, []foundryhttp.Middleware, error) {
				d, registry, connection, err := declared(r)
				if err != nil {
					return nil, nil, err
				}
				hub, err := connection.NewHub(registry, d.Authentication, s.Realtime.Config, d.hubOptions()...)
				return hub, d.Middleware, err
			}))
		}
	}
	return nil
}
func (s Services) Events() (*events.Bus, error)            { return Resolve(s, EventKey) }
func (s Services) Scheduler() (*schedule.Scheduler, error) { return Resolve(s, SchedulerKey) }
func (s Services) RealtimeHub() (*websocket.Hub, error)    { return Resolve(s, RealtimeKey) }

// RealtimePublisher returns the hub when this process serves realtime, or the
// managed cross-process publisher when Realtime.Publisher is enabled. Both accept
// Publish, Broadcast and disconnect operations for HTTP handlers and jobs.
func (s Services) RealtimePublisher() (websocket.PublisherSource, error) {
	if hub, err := Resolve(s, RealtimeKey); err == nil {
		return hub, nil
	}
	publisher, err := Resolve(s, RealtimePublisherKey)
	if err != nil {
		return nil, fault.New(fault.Missing, "realtime publication is not configured")
	}
	return publisher, nil
}

// realtimeRoutes mounts the shared-listener upgrade route with its declared
// middleware on the application router.
func realtimeRoutes(s Settings) Routes {
	if !s.Realtime.Enabled || !s.Realtime.Shared {
		return nil
	}
	return func(services Services) ([]foundryhttp.RouteRegistration, error) {
		hub, err := Resolve(services, RealtimeKey)
		if err != nil {
			return nil, err
		}
		d, err := Resolve(services, realtimeDeclarationsKey)
		if err != nil {
			return nil, err
		}
		return []foundryhttp.RouteRegistration{websocket.Route(hub, s.Realtime.Path, d.Middleware...)}, nil
	}
}
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

const EventListenerQueueProvider foundation.ProviderID = "foundry.application.events.listener-queue"

var eventListenerQueueKey = foundation.NewKey[*events.ListenerQueue](string(EventListenerQueueProvider))

// registerListenerQueue delivers queued event listeners as jobs on the
// configured connection and binds its dispatcher at boot.
func registerListenerQueue(builder *foundation.Builder, s Settings) error {
	connection := s.Features.Events.ListenerConnection
	if connection == "" {
		connection = s.Services.Jobs.Default
	}
	if _, ok := s.Services.Jobs.Connections[connection]; !ok {
		return fault.New(fault.Missing, "queued event listener connection is not configured")
	}
	queue := s.Features.Events.ListenerQueue
	if queue == "" {
		queue = "default"
	}
	policy := jobs.DefaultPolicy(queue)
	if err := policy.Validate(); err != nil {
		return err
	}
	builder.Register(foundation.Module{Name: EventListenerQueueProvider, Requires: []foundation.ProviderID{EventProvider, infrastructure.JobProvider(connection)}, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, eventListenerQueueKey, func(resolver foundation.Resolver) (*events.ListenerQueue, error) {
			bus, err := foundation.Resolve(resolver, EventKey)
			if err != nil {
				return nil, err
			}
			return events.NewListenerQueue(bus, policy)
		}); err != nil {
			return err
		}
		return jobs.RegisterJob(r, infrastructure.JobDispatcherKey(connection), events.ListenerDefinition(policy), func(resolver foundation.Resolver) (jobs.Declaration, error) {
			queue, err := foundation.Resolve(resolver, eventListenerQueueKey)
			if err != nil {
				return jobs.Declaration{}, err
			}
			return queue.Declaration()
		})
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		queue, err := foundation.Resolve(r.Services(), eventListenerQueueKey)
		if err != nil {
			return err
		}
		dispatcher, err := foundation.Resolve(r.Services(), infrastructure.JobDispatcherKey(connection))
		if err != nil {
			return err
		}
		return queue.Bind(dispatcher)
	}})
	return nil
}

const JobArchiveProvider foundation.ProviderID = "foundry.application.jobs.archive"

var JobArchiveKey = foundation.NewKey[*archive.Store](string(JobArchiveProvider))

// JobArchive returns the durable failed-job archive when it is enabled.
func (s Services) JobArchive() (*archive.Store, error) { return Resolve(s, JobArchiveKey) }

// registerJobArchive builds the archive store and attaches it as a failure
// sink to the worker's connection.
func registerJobArchive(builder *foundation.Builder, s Settings, source clock.Clock) error {
	// prepareJobArchive already selected and checked the database and schema.
	name, schema := s.Worker.Archive.Database, s.Worker.Archive.Schema
	connection := s.Worker.Connection
	if connection == "" {
		connection = s.Services.Jobs.Default
	}
	if _, ok := s.Services.Jobs.Connections[connection]; !ok {
		return fault.New(fault.Missing, "job archive worker connection is not configured")
	}
	builder.Register(foundation.Module{Name: JobArchiveProvider, Requires: []foundation.ProviderID{infrastructure.DatabaseProvider(name), infrastructure.JobProvider(connection)}, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, JobArchiveKey, func(resolver foundation.Resolver) (*archive.Store, error) {
			db, err := foundation.Resolve(resolver, infrastructure.DatabaseKey(name))
			if err != nil {
				return nil, err
			}
			return archive.New(db, schema, source)
		}); err != nil {
			return err
		}
		return jobs.RegisterFailureSink(r, infrastructure.JobDispatcherKey(connection), "archive", func(resolver foundation.Resolver) (jobs.FailureSink, error) {
			return foundation.Resolve(resolver, JobArchiveKey)
		})
	}})
	return nil
}
