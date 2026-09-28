package diagnostics_test

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
)

func TestDiagnosticsModuleBindsLifecycleAfterPureAssembly(t *testing.T) {
	probes := foundation.NewKey[*health.Registry]("readiness")
	status := foundation.NewKey[*diagnostics.Runtime]("diagnostics")
	app, err := foundation.NewBuilder().Register(
		health.Module("readiness", probes, health.DefaultConfig(), nil, func(foundation.Resolver) ([]health.Probe, error) { return nil, nil }),
		diagnostics.Module("diagnostics", status, probes, diagnostics.DefaultConfig(), []foundation.ProviderID{"readiness"}),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	runtime, err := foundation.Resolve(app.Services(), status)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Snapshot().State != foundation.Prepared {
		t.Fatal("pure assembly inferred a running app")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := runtime.Readiness(t.Context()); err != nil || !report.Ready {
		t.Fatal("module did not bind actual running state", err, report)
	}
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if runtime.Liveness().Live || runtime.Snapshot().State != foundation.Stopped {
		t.Fatal("stopped app retained live diagnostics")
	}
}
