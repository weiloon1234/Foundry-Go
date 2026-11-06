package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

func telemetryFixtureSettings() Settings {
	s := DefaultSettings()
	s.HTTP.Enabled = false
	s.Features.Observability.Enabled = true
	s.Features.Telemetry = TelemetrySettings{Enabled: true,
		Collector: telemetry.CollectorConfig{Instance: "test", Release: "test", MaxSeries: 16, MaxEvents: 16, MaxPendingBatches: 4, PendingRetention: time.Minute},
		Config: telemetry.Config{FlushInterval: time.Second, FlushTimeout: time.Second, DrainTimeout: time.Second,
			Retention: telemetry.RetentionPolicy{Minute: time.Hour, Hour: 24 * time.Hour, Issue: 24 * time.Hour, Security: time.Hour, MaxRuns: 100, MaxReceiptRows: 1000, MaxMetricRows: 1000, MaxIssueRows: 100, MaxEventRows: 100, PruneInterval: time.Minute},
			Reads:     telemetry.ReadLimits{MaxConcurrent: 2, MaxOverviewRuns: 10, MaxOverviewAlerts: 10, MaxTrafficRows: 1000, MaxTrafficGroups: 10, MaxPage: 10, MaxPageSize: 10, Timeout: time.Second}}}
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary.Host, connection.Primary.Database, connection.Primary.User = "127.0.0.1", "fixture", "fixture"
	connection.Primary.Port = 1 // Build must not attempt a connection.
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	return s
}

func telemetryQuiet() Option {
	return WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func closeTelemetryApp(t *testing.T, app *App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}

func TestDisabledTelemetryCreatesNoDeclarations(t *testing.T) {
	s := DefaultSettings()
	s.HTTP.Enabled = false
	s.Features.Telemetry = TelemetrySettings{Database: "absent", Schema: "invalid-schema", Collector: telemetry.CollectorConfig{MaxSeries: -1}}
	app, err := New(s, telemetryQuiet()).TelemetryReadiness(foundation.NewKey[*diagnostics.Runtime]("unregistered.readiness")).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTelemetryApp(t, app) })
	if _, err := app.Resources().Telemetry(); !errors.Is(err, fault.Missing) {
		t.Fatal("disabled telemetry resolved an engine", err)
	}
	if app.Resources().Observability() != nil {
		t.Fatal("disabled telemetry created a recorder")
	}
	for _, provider := range app.Inspect().Providers {
		if provider.ID == TelemetryProvider {
			t.Fatal("disabled telemetry registered a provider")
		}
	}
	for _, target := range app.Migrations() {
		for _, definition := range target.Definitions {
			if definition.Key.Origin == "foundry.telemetry" {
				t.Fatal("disabled telemetry added a migration")
			}
		}
	}
}

func TestTelemetrySettingsAreDeepSnapshots(t *testing.T) {
	s := telemetryFixtureSettings()
	f := &s.Features.Telemetry
	f.Kernels = []foundation.KernelKind{foundation.Worker}
	f.Collector.Routes = telemetry.RoutePolicy{ExcludedRoutes: []http.RouteID{"excluded"}, ExcludedPrefixes: []string{"prefix."}, ExcludedOperations: []observability.Name{"operation"}, ExcludedOperationPrefixes: []string{"operation."}}
	f.Collector.Browser = telemetry.BrowserPolicy{Enabled: true, Portals: []telemetry.Portal{"portal"}}
	f.Config.Alerts.Rules = []telemetry.Rule{telemetry.RuleLatency}
	b := New(s, telemetryQuiet())
	f.Kernels[0] = foundation.CLI
	f.Collector.Routes.ExcludedRoutes[0] = "changed"
	f.Collector.Routes.ExcludedPrefixes[0] = "changed."
	f.Collector.Routes.ExcludedOperations[0] = "changed"
	f.Collector.Routes.ExcludedOperationPrefixes[0] = "changed."
	f.Collector.Browser.Portals[0] = "changed"
	f.Config.Alerts.Rules[0] = telemetry.RuleHTTPErrors
	if b.state.err != nil {
		t.Fatal(b.state.err)
	}
	got := b.state.settings.Features.Telemetry
	if got.Kernels[0] != foundation.Worker || got.Collector.Routes.ExcludedRoutes[0] != "excluded" || got.Collector.Routes.ExcludedPrefixes[0] != "prefix." || got.Collector.Routes.ExcludedOperations[0] != "operation" || got.Collector.Routes.ExcludedOperationPrefixes[0] != "operation." || got.Collector.Browser.Portals[0] != "portal" || got.Config.Alerts.Rules[0] != telemetry.RuleLatency {
		t.Fatal("builder retained caller-owned telemetry settings")
	}
}

func TestTelemetryBuildValidationAndMigrationDefaults(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Settings)
		code error
	}{
		{"observability required", func(s *Settings) { s.Features.Observability.Enabled = false }, fault.Invalid},
		{"unknown database", func(s *Settings) { s.Features.Telemetry.Database = "absent" }, fault.Missing},
		{"invalid schema", func(s *Settings) { s.Features.Telemetry.Schema = "invalid-schema" }, fault.Invalid},
		{"duplicate kernels", func(s *Settings) {
			s.Features.Telemetry.Kernels = []foundation.KernelKind{foundation.Worker, foundation.Worker}
		}, fault.Duplicate},
		{"unknown kernel", func(s *Settings) { s.Features.Telemetry.Kernels = []foundation.KernelKind{"unknown"} }, fault.Invalid},
		{"collector bounds", func(s *Settings) { s.Features.Telemetry.Collector.MaxSeries = 0 }, fault.Invalid},
		{"engine bounds", func(s *Settings) { s.Features.Telemetry.Config.Reads.MaxConcurrent = 0 }, fault.Invalid},
		{"receipt horizon", func(s *Settings) {
			s.Features.Telemetry.Collector.PendingRetention = time.Hour
			s.Features.Telemetry.Config.Retention.Hour = time.Hour
		}, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := telemetryFixtureSettings()
			test.edit(&s)
			if _, err := New(s, telemetryQuiet()).Build(t.Context()); !errors.Is(err, test.code) {
				t.Fatal("invalid telemetry configuration passed Build", err)
			}
		})
	}
	for _, schema := range []string{"", "scoped_fixture"} {
		t.Run("schema_"+schema, func(t *testing.T) {
			s := telemetryFixtureSettings()
			connection := s.Services.Database.Connections["default"]
			connection.Primary.Schema = schema
			s.Services.Database.Connections["default"] = connection
			app, err := New(s, telemetryQuiet()).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeTelemetryApp(t, app) })
			if _, err := app.Resources().Telemetry(); err != nil {
				t.Fatal(err)
			}
			want := schema
			if want == "" {
				want = "public"
			}
			found := false
			for _, target := range app.Migrations() {
				for _, definition := range target.Definitions {
					if definition.Key.Origin == "foundry.telemetry" {
						found = true
						if target.Schema != want || target.Connection != "default" {
							t.Fatal("migration target lost default resolution")
						}
					}
				}
			}
			if !found {
				t.Fatal("enabled telemetry migration missing")
			}
		})
	}
}

func TestTelemetryRejectsSuppliedRecorder(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Close(context.Background()) })
	if _, err := New(telemetryFixtureSettings(), telemetryQuiet(), WithObservability(recorder)).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("telemetry silently failed to attach its reporter", err)
	}
}

func TestTelemetryReadinessUsesDeclaredProviderOwnership(t *testing.T) {
	const owner foundation.ProviderID = "fixture.readiness-owner"
	key := foundation.NewKey[*diagnostics.Runtime]("fixture.unrelated-service-name")
	probes := foundation.NewKey[*health.Registry]("fixture.probes")
	b := New(telemetryFixtureSettings(), telemetryQuiet()).Register(
		health.Module("fixture.probes-owner", probes, health.DefaultConfig(), nil, func(foundation.Resolver) ([]health.Probe, error) { return nil, nil }),
		diagnostics.Module(owner, key, probes, diagnostics.DefaultConfig(), []foundation.ProviderID{"fixture.probes-owner"}),
	).TelemetryReadiness(key)
	app, err := b.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTelemetryApp(t, app) })
	for _, provider := range app.Inspect().Providers {
		if provider.ID == TelemetryProvider && slices.Contains(provider.Dependencies, owner) {
			return
		}
	}
	t.Fatal("telemetry readiness provider was not an explicit boot dependency")
}

func TestTelemetryReadinessRejectsMissingWrongAndDuplicateKeys(t *testing.T) {
	key := foundation.NewKey[*diagnostics.Runtime]("fixture.readiness")
	if _, err := New(telemetryFixtureSettings(), telemetryQuiet()).TelemetryReadiness(key).Build(t.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	wrong := foundation.Module{Name: "fixture.wrong", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, foundation.NewKey[string](key.Name()), "wrong")
	}}
	if _, err := New(telemetryFixtureSettings(), telemetryQuiet()).Register(wrong).TelemetryReadiness(key).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := New(telemetryFixtureSettings(), telemetryQuiet()).TelemetryReadiness(key).TelemetryReadiness(key).Build(t.Context()); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
}
