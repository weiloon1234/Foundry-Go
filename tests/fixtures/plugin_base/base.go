// Package pluginbase is an independent plugin module used by framework
// acceptance. It imports only public Foundry packages and owns no global runtime.
package pluginbase

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

const ID plugin.ID = "fixture_base"
const InitialMigration migrate.ID = "001_base"

type Settings struct{ Prefix string }

func DefaultSettings() Settings { return Settings{Prefix: "base"} }

var Prefix = config.String("plugins.fixture_base.prefix", func(value *Settings) *string { return &value.Prefix })
var Title = foundation.NewKey[string]("fixture.base.title")
var Configuration = foundation.NewKey[config.Report]("fixture.base.config_sources")
var Journal = foundation.NewKey[*Trace]("fixture.base.trace")
var Router = foundation.NewKey[*foundryhttp.Router]("fixture.http.router")
var Authorization = foundation.NewKey[*auth.Registry]("fixture.auth.registry")
var Migrations = foundation.NewKey[*migrate.Registry]("fixture.migrations")
var Dispatcher = foundation.NewKey[*jobs.Dispatcher]("fixture.jobs")
var Bus = foundation.NewKey[*events.Bus]("fixture.events")
var backendKey = foundation.NewKey[*memory.Backend]("fixture.jobs.backend")

func Declaration() plugin.Manifest {
	return plugin.Manifest{ID: ID, Version: "2.0.0", Framework: "^0.1.0", Description: "Independent base plugin fixture"}
}

func New(inputs config.Inputs[Settings]) (plugin.Module, error) {
	schema, err := config.New(Prefix)
	if err != nil {
		return plugin.Module{}, err
	}
	settings, report, err := plugin.LoadConfig(Declaration(), schema, DefaultSettings(), inputs)
	if err != nil {
		return plugin.Module{}, err
	}
	namespace := keyspace.Namespace{Application: "plugin-fixture", Environment: "test"}
	eventModule := events.Module("fixture.events", Bus, events.DefaultConfig())
	jobModule := jobs.Module("fixture.jobs", Dispatcher, jobs.DefaultDispatchConfig(namespace), jobs.DefaultWorkerConfig(namespace, "default"), nil, func(resolver foundation.Resolver) (jobs.Backend, []jobs.Declaration, error) {
		backend, err := foundation.Resolve(resolver, backendKey)
		return backend, nil, err
	})
	return plugin.Module{Declaration: Declaration(), OnRegister: func(r *plugin.Registrar) error {
		if err := foundation.Provide(r, Title, settings.Prefix); err != nil {
			return err
		}
		if err := foundation.Provide(r, Configuration, report); err != nil {
			return err
		}
		if err := foundation.Factory(r, Journal, func(foundation.Resolver) (*Trace, error) { return NewTrace(), nil }); err != nil {
			return err
		}
		if err := foundation.Factory(r, backendKey, func(foundation.Resolver) (*memory.Backend, error) { return memory.New(memory.DefaultConfig()) }); err != nil {
			return err
		}
		if err := foundryhttp.RegisterRouter(r, Router); err != nil {
			return err
		}
		if err := auth.RegisterRegistry(r, Authorization, auth.DefaultConfig()); err != nil {
			return err
		}
		if err := plugin.RegisterMigrationRegistry(r, Migrations); err != nil {
			return err
		}
		if err := plugin.RegisterMigrations(r, Migrations, migrate.Definition{Key: migrate.Key{ID: InitialMigration}, Version: "1.0.0", SQL: []string{"SELECT 1"}}); err != nil {
			return err
		}
		if err := jobModule.Register(r); err != nil {
			return err
		}
		return eventModule.Register(r)
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		backend, err := foundation.Resolve(r.Services(), backendKey)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("jobs", func(context.Context) error { return backend.Close() }); err != nil {
			return err
		}
		if err := eventModule.Boot(ctx, r); err != nil {
			return err
		}
		trace, err := foundation.Resolve(r.Services(), Journal)
		if err != nil {
			return err
		}
		trace.Record("base.boot")
		return nil
	}, OnShutdown: func(_ context.Context, r *foundation.Runtime) error {
		trace, err := foundation.Resolve(r.Services(), Journal)
		if err != nil {
			return err
		}
		trace.Record("base.shutdown")
		return nil
	}}, nil
}
