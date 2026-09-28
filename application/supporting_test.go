package application_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/websocket"
	stdhttp "net/http"
	"sync/atomic"
	"testing"
	"time"
)

type assemblyPayload struct {
	Value string `json:"value"`
}

func TestSelectedWorkerConsumesOnlyItsConfiguredConnection(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Worker.Enabled = true
	s.Worker.Connection = "second"
	s.Worker.Config.PollInterval = time.Millisecond
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": infrastructure.DefaultJobConnectionSettings(), "second": infrastructure.DefaultJobConnectionSettings()}
	c := s.Services.Jobs.Connections["second"]
	c.DefaultQueue = "reports"
	s.Services.Jobs.Connections["second"] = c
	definition := jobs.Define[assemblyPayload]("assembly", 1, jobs.DefaultPolicy("legacy"))
	received := make(chan string, 4)
	constructor := func(application.Services) (jobs.Handler[assemblyPayload], error) {
		return func(_ context.Context, p assemblyPayload) error { received <- p.Value; return nil }, nil
	}
	app, err := application.New(s, quiet()).Jobs(application.Job(definition, constructor), application.Job(definition, constructor).On("second")).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.Worker) }()
	first, _ := app.Resources().JobConnection()
	second, _ := app.Resources().Jobs.Connection("second")
	a, _ := definition.On(first)
	b, _ := definition.On(second)
	if _, err := a.Dispatch(t.Context(), assemblyPayload{"unselected"}, jobs.Options[assemblyPayload]{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Dispatch(t.Context(), assemblyPayload{"selected"}, jobs.Options[assemblyPayload]{}); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-received:
		if value != "selected" {
			t.Fatal("wrong connection consumed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not consume configured queue")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not drain")
	}
	select {
	case <-received:
		t.Fatal("unselected connection consumed")
	default:
	}
}
func TestEventsAndSchedulerUseConstructorServices(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Features.Events.Enabled = true
	s.Services.Coordination.Enabled = true
	s.Scheduler.Enabled = true
	topic := events.Define[assemblyPayload]("assembled", 1)
	var delivered atomic.Int32
	app, err := application.New(s, quiet()).Events(application.Listen(topic, "fixture", func(services application.Services) (events.Handler[assemblyPayload], error) {
		if _, err := services.Cache(); err != nil {
			return nil, err
		}
		return func(context.Context, assemblyPayload) error { delivered.Add(1); return nil }, nil
	})).Schedules(func(services application.Services) ([]schedule.Declaration, error) {
		if _, err := services.Leases(); err != nil {
			return nil, err
		}
		d, err := schedule.Every("fixture", time.Hour, func(context.Context, schedule.Invocation) error { return nil })
		return []schedule.Declaration{d}, err
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	bus, err := app.Resources().Events()
	if err != nil {
		t.Fatal(err)
	}
	if err := topic.Dispatch(t.Context(), bus, assemblyPayload{"one"}); err != nil || delivered.Load() != 1 {
		t.Fatal("listener not bound", err)
	}
	if _, err := app.Resources().Scheduler(); err != nil {
		t.Fatal(err)
	}
}
func TestRealtimeDeclarationsArePureAndSelectOneServer(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Realtime.Enabled = true
	s.Realtime.Connection = "second"
	s.Realtime.HTTP.Address = "127.0.0.1:0"
	s.Services.Realtime.Connections = infrastructure.RealtimeConnections{"default": infrastructure.DefaultRealtimeConnectionSettings(), "second": infrastructure.DefaultRealtimeConnectionSettings()}
	channel := websocket.Public[struct{}]("fixture", websocket.DefineRooms(http.StringPath[string]()))
	var constructed atomic.Int32
	middleware := http.DefineMiddleware("fixture.realtime", func(next stdhttp.Handler) (stdhttp.Handler, error) { constructed.Add(1); return next, nil })
	app, err := application.New(s, quiet()).Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel)}, Middleware: []http.Middleware{middleware}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if constructed.Load() != 1 {
		t.Fatal("middleware must be constructed once during Build")
	}
	t.Cleanup(func() { stop(t, app) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.WebSocket) }()
	ready, err := app.RealtimeReady(t.Context())
	if err != nil || ready == "" {
		t.Fatal(err)
	}
	if constructed.Load() != 1 {
		t.Fatal("kernel rebuilt middleware")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("realtime did not drain")
	}
	if _, err := application.New(s, quiet()).Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel)}, Middleware: []http.Middleware{middleware, middleware}}, nil
	}).Build(t.Context()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate middleware must fail pure Build", err)
	}
}
func TestFeatureDeclarationsRejectDisabledManagers(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	topic := events.Define[assemblyPayload]("disabled", 1)
	if _, err := application.New(s, quiet()).Events(application.Topic(topic)).Build(t.Context()); err == nil {
		t.Fatal("disabled events accepted")
	}
	if _, err := application.New(s, quiet()).Schedules(func(application.Services) ([]schedule.Declaration, error) { return nil, nil }).Build(t.Context()); err == nil {
		t.Fatal("disabled scheduler accepted")
	}
}

func TestConstructorServicesExpireButRuntimeResourcesRemainResolvable(t *testing.T) {
	s := settings()
	s.Features.Events.Enabled = true
	var escaped application.Services
	app, err := application.New(s, quiet()).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
		escaped = services
		if _, err := services.Events(); err != nil {
			return nil, err
		}
		return plainRoutes(services)
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if _, err := escaped.Events(); !errors.Is(err, fault.Closed) {
		t.Fatal("constructor facade retained an active resolver", err)
	}
	bus, err := app.Resources().Events()
	if err != nil || bus == nil {
		t.Fatal("runtime facade retained an expired constructor", err)
	}
	facade, err := application.FromResolver(app.Services())
	if err != nil {
		t.Fatal(err)
	}
	alias, err := facade.Events()
	if err != nil || alias != bus {
		t.Fatal("typed runtime facade duplicated a service", err)
	}
}
