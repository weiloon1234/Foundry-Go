package application

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func telemetryPostgresSettings(t *testing.T) Settings {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	registry, err := migrate.New(telemetry.Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	cfg := migrate.DefaultPostgresConfig()
	cfg.Schema, cfg.SearchPath = scope.Schema(), scope.Schema()
	runner, err := migrate.NewPostgres(db, registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := telemetryFixtureSettings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(scope.Config())
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	return s
}

func TestPostgresTelemetryDefaultKernelSelection(t *testing.T) {
	for _, kind := range []foundation.KernelKind{foundation.HTTP, foundation.Worker, foundation.Scheduler, foundation.CLI} {
		t.Run(string(kind), func(t *testing.T) {
			s := telemetryPostgresSettings(t)
			seen := make(chan bool, 1)
			var app *App
			module := foundation.Module{Name: "fixture.telemetry-kernel", OnRegister: func(r *foundation.Registrar) error {
				return r.Kernel(kind, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
					return foundation.KernelFunc(func(context.Context) error {
						services, err := FromResolver(runtime.Services())
						if err != nil {
							return err
						}
						if _, err := services.Telemetry(); err != nil {
							return err
						}
						// Kernel tasks start only after every provider has booted.
						seen <- strings.Contains(strings.Join(app.PendingTasks(), ","), "provider[foundry.application.telemetry]")
						return nil
					}), nil
				})
			}}
			var err error
			app, err = New(s, telemetryQuiet()).Register(module).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeTelemetryApp(t, app) })
			if err := app.Run(t.Context(), kind); err != nil {
				t.Fatal(err)
			}
			if got := <-seen; got != (kind != foundation.CLI) {
				t.Fatal("telemetry selected the wrong process kernel", got)
			}
			engine, err := app.Resources().Telemetry()
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-engine.Done():
			default:
				t.Fatal("kernel exit left telemetry ownership alive")
			}
		})
	}
}

func TestPostgresTelemetryFailedBootCleansBeforeDatabaseAndRecorder(t *testing.T) {
	s := telemetryPostgresSettings(t)
	boom := errors.New("fixture boot failure")
	var app *App
	var engine *telemetry.Engine
	cleanupBeforeTelemetry := false
	failed := foundation.Module{Name: "fixture.after-telemetry", Requires: []foundation.ProviderID{TelemetryProvider}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		var err error
		engine, err = foundation.Resolve(r.Services(), TelemetryKey)
		if err != nil {
			return err
		}
		if err := r.OnShutdown("before-telemetry", func(context.Context) error {
			select {
			case <-engine.Done():
				return errors.New("telemetry cleaned before its dependent")
			default:
			}
			select {
			case <-r.Observability().Done():
				return errors.New("recorder closed before telemetry cleanup")
			default:
			}
			db, err := app.Resources().Database()
			if err != nil {
				return err
			}
			cleanupBeforeTelemetry = db.Stats().Ready
			return nil
		}); err != nil {
			return err
		}
		return boom
	}}
	var err error
	app, err = New(s, telemetryQuiet()).Register(failed).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx) // The expected boot failure is asserted below.
	})
	if err := app.Start(t.Context()); !errors.Is(err, boom) {
		t.Fatal("failed boot lost its cause", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); !errors.Is(err, boom) {
		t.Fatal("failed boot did not complete cleanup", err)
	}
	if !cleanupBeforeTelemetry {
		t.Fatal("database closed before dependent cleanup")
	}
	select {
	case <-engine.Done():
	default:
		t.Fatal("partial boot leaked engine ownership")
	}
	select {
	case <-app.Observability().Done():
	default:
		t.Fatal("partial boot leaked recorder ownership")
	}
	db, err := app.Resources().Database()
	if err != nil || db.Stats().Ready {
		t.Fatal("partial boot leaked database", err)
	}
}

func TestPostgresTelemetryObservesConfiguredHTTPAndReporter(t *testing.T) {
	s := telemetryPostgresSettings(t)
	s.HTTP.Enabled = true
	s.HTTP.Server.Address = "127.0.0.1:0"
	routes := func(Services) ([]http.RouteRegistration, error) {
		return []http.RouteRegistration{http.HandleRaw(http.DefineRoute(http.RouteSpec{ID: "fixture.denied", Method: http.GET, Access: http.Public}, http.StaticPath("/denied")), func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ http.NoPath) { w.WriteHeader(401) })}, nil
	}
	app, err := New(s, telemetryQuiet()).HTTP(routes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	t.Cleanup(func() { cancel(); closeTelemetryApp(t, app) })
	ready, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	address, err := app.HTTPReady(ready)
	if err != nil {
		t.Fatal(err)
	}
	response, err := stdhttp.Get("http://" + address + "/denied?token=private-canary")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("fixture HTTP status changed")
	}
	_ = observability.Observe(observability.WithContext(t.Context(), app.Observability()), observability.Operation{Kind: observability.Job, Name: "fixture.failed-job"}, func(context.Context) error { return fault.Internal })
	engine, err := app.Resources().Telemetry()
	if err != nil {
		t.Fatal(err)
	}
	reader := engine.Reader()
	wait, finish := context.WithTimeout(t.Context(), 5*time.Second)
	defer finish()
	for {
		traffic, err := reader.Traffic(wait, telemetry.TrafficQuery{Window: telemetry.Window{Minutes: 15}, IncludeExcluded: true})
		if err != nil {
			t.Fatal(err)
		}
		security, err := reader.Issues(wait, telemetry.IssueQuery{Source: telemetry.SourceSecurity, Page: orm.Page(1, 10)})
		if err != nil {
			t.Fatal(err)
		}
		backend, err := reader.Issues(wait, telemetry.IssueQuery{Source: telemetry.SourceBackend, Page: orm.Page(1, 10)})
		if err != nil {
			t.Fatal(err)
		}
		if len(traffic.Items) > 0 && len(security.Items) > 0 && len(backend.Items) > 0 {
			if traffic.Items[0].Route != "fixture.denied" || traffic.Items[0].RoutePath != "/denied" {
				t.Fatal("collector lost the assembled route inventory")
			}
			if security.Items[0].Code != "authentication_failed" || security.Items[0].Detail.Path != "/[redacted]" {
				t.Fatal("security observer was not attached or retained a raw URL")
			}
			break
		}
		select {
		case <-wait.Done():
			t.Fatal("HTTP/security/reporter observations were not persisted")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPostgresConfiguredTelemetryAdmitsBrowserReports(t *testing.T) {
	s := telemetryPostgresSettings(t)
	s.Features.Telemetry.Collector.Browser = telemetry.BrowserPolicy{Enabled: true, Portals: []telemetry.Portal{"admin"}}
	app, err := New(s, telemetryQuiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTelemetryApp(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	engine, err := app.Resources().Telemetry()
	if err != nil {
		t.Fatal(err)
	}
	report := telemetry.BrowserError{
		Portal: "admin", Release: s.Features.Telemetry.Collector.Release,
		Code:   telemetry.BrowserCodeTypeError,
		Frames: []telemetry.BrowserFrame{{File: "admin/_assets/app.js", Line: 1}},
	}
	invalid := report
	invalid.Portal = "unconfigured"
	if err := engine.RecordBrowser(t.Context(), invalid); !errors.Is(err, fault.Invalid) {
		t.Fatalf("configured browser validation: %v", err)
	}
	if err := engine.RecordBrowser(t.Context(), report); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := engine.RecordBrowser(t.Context(), report); err != nil {
		t.Fatalf("configured browser admission after Close: %v", err)
	}
}
