package teamworkflow

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

func Defaults() application.Settings {
	settings := application.DefaultSettings()
	settings.HTTP.Server.Address = "127.0.0.1:0"
	settings.Services.Database.Default = "main"
	connection := infrastructure.DefaultConnectionSettings()
	settings.Services.Database.Connections = infrastructure.DatabaseConnections{"main": connection, "receiver": connection}
	settings.Features.Events.Enabled = true
	settings.Features.Outbox.Enabled = true
	settings.Features.Idempotency.Enabled = true
	return settings
}

// Build uses configured defaults and a named receiver. Constructors receive
// concrete dependencies during assembly; handlers retain them after sealing.
func Build(ctx context.Context, settings application.Settings, hooks Hooks, options ...application.Option) (*application.App, error) {
	return application.New(settings, options...).Events(application.Topic(Submitted)).Features(func(services application.Services) (application.FeatureDeclarations, error) {
		receiver, err := services.Databases.Connection("receiver")
		if err != nil {
			return application.FeatureDeclarations{}, err
		}
		sink := NewDeliverySink(receiver)
		return application.FeatureDeclarations{Outbox: []publisher.Route{sink.Route(hooks.Delivered)}}, nil
	}).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
		db, err := services.Database()
		if err != nil {
			return nil, err
		}
		store, err := services.Idempotency()
		if err != nil {
			return nil, err
		}
		bus, err := services.Events()
		if err != nil {
			return nil, err
		}
		service, err := NewService(db, store, bus, hooks)
		if err != nil {
			return nil, err
		}
		return service.Routes()
	}).Build(ctx)
}
