// Package plugindep is an independently built plugin depending on pluginbase.
// Application consumers register it directly without writing Provider wrappers.
package plugindep

import (
	"context"
	"fmt"
	stdhttp "net/http"

	"foundry.test/pluginbase"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/plugin"
	"github.com/weiloon1234/Foundry-Go/plugin/assets"
	"github.com/weiloon1234/Foundry-Go/plugin/scaffold"
)

const ID plugin.ID = "fixture_reports"
const ReportMigration migrate.ID = "002_reports"

type Payload struct {
	Text string `json:"text"`
}
type Subject struct{ ID int64 }
type Report struct{ Owner int64 }

var Route = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "fixture.reports.index", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/reports"))
var Job = jobs.Define[Payload]("fixture.reports.render", 1, jobs.DefaultPolicy("default"))
var Changed = events.Define[Payload]("fixture.reports.changed", 1)
var CanRead = auth.DefinePolicy("fixture.reports.read", func(_ context.Context, subject Subject, report Report) (bool, error) {
	return subject.ID == report.Owner, nil
})

func Declaration() plugin.Manifest {
	return plugin.Manifest{ID: ID, Version: "1.1.0", Framework: "^0.1.0", Dependencies: []plugin.Dependency{{ID: pluginbase.ID, Version: "^2.0.0"}}}
}

func New() (plugin.Module, error) {
	bundle, err := assets.New(Declaration(), "public", assets.File{Path: "css/reports.css", Data: []byte(".reports { display: grid; }\n")})
	if err != nil {
		return plugin.Module{}, err
	}
	code, err := scaffold.New(Declaration(), "report", renderReport)
	if err != nil {
		return plugin.Module{}, err
	}
	return plugin.Module{Declaration: Declaration(), OnRegister: func(r *plugin.Registrar) error {
		if err := foundryhttp.RegisterRoute(r, pluginbase.Router, Route, func(resolver foundation.Resolver) (foundryhttp.RouteRegistration, error) {
			title, err := foundation.Resolve(resolver, pluginbase.Title)
			if err != nil {
				return foundryhttp.RouteRegistration{}, err
			}
			return Route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ foundryhttp.NoPath) {
				_, _ = fmt.Fprint(w, title+" reports")
			}), nil
		}); err != nil {
			return err
		}
		if err := foundryhttp.RegisterMiddleware(r, pluginbase.Router, foundryhttp.DefineMiddleware("fixture.reports.header", func(next stdhttp.Handler) (stdhttp.Handler, error) {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
				w.Header().Set("X-Plugin", "reports")
				next.ServeHTTP(w, request)
			}), nil
		})); err != nil {
			return err
		}
		if err := jobs.RegisterJob(r, pluginbase.Dispatcher, Job, func(resolver foundation.Resolver) (jobs.Declaration, error) {
			trace, err := foundation.Resolve(resolver, pluginbase.Journal)
			if err != nil {
				return jobs.Declaration{}, err
			}
			// The producer can be injected into its own handler without a
			// service construction cycle, for chained domain work.
			if _, err := foundation.Resolve(resolver, pluginbase.Dispatcher); err != nil {
				return jobs.Declaration{}, err
			}
			return Job.Declare(func(_ context.Context, payload Payload) error { trace.Record("job:" + payload.Text); return nil })
		}); err != nil {
			return err
		}
		if err := events.RegisterListener(r, pluginbase.Bus, Changed, "fixture.reports.record", func(resolver foundation.Resolver) (events.Handler[Payload], error) {
			trace, err := foundation.Resolve(resolver, pluginbase.Journal)
			if err != nil {
				return nil, err
			}
			return func(_ context.Context, payload Payload) error { trace.Record("event:" + payload.Text); return nil }, nil
		}); err != nil {
			return err
		}
		if err := auth.RegisterAuthorization(r, pluginbase.Authorization, CanRead); err != nil {
			return err
		}
		if err := plugin.RegisterMigrations(r, pluginbase.Migrations, migrate.Definition{Key: migrate.Key{ID: ReportMigration}, Version: "1.0.0", SQL: []string{"SELECT 2"}, Requires: []migrate.Key{{Origin: plugin.MigrationOrigin(pluginbase.ID), ID: pluginbase.InitialMigration}}}); err != nil {
			return err
		}
		if err := assets.Register(r, bundle); err != nil {
			return err
		}
		return scaffold.Register(r, code)
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		trace, err := foundation.Resolve(r.Services(), pluginbase.Journal)
		if err != nil {
			return err
		}
		trace.Record("reports.boot")
		return nil
	}, OnShutdown: func(_ context.Context, r *foundation.Runtime) error {
		trace, err := foundation.Resolve(r.Services(), pluginbase.Journal)
		if err != nil {
			return err
		}
		trace.Record("reports.shutdown")
		return nil
	}}, nil
}
