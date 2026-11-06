package application

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	telemetrytest "github.com/weiloon1234/Foundry-Go/testkit/telemetry"
)

func TestPostgresConfiguredTelemetryTestFlushWithoutKernel(t *testing.T) {
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
	if err := engine.RecordBrowser(t.Context(), telemetry.BrowserError{
		Portal: "admin", Release: s.Features.Telemetry.Collector.Release, Code: telemetry.BrowserCodeTypeError,
		Frames: []telemetry.BrowserFrame{{File: "admin/_assets/app.js", Line: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		telemetrytest.Flush(t, engine)
		issues, err := engine.Reader().Issues(t.Context(), telemetry.IssueQuery{Source: telemetry.SourceBrowser, Page: orm.Page(1, 10)})
		if err != nil {
			t.Fatal(err)
		}
		if issues.Total != 1 || len(issues.Items) != 1 || issues.Items[0].Count != 1 {
			t.Fatalf("browser issue after synchronous flush: %+v", issues)
		}
		events, err := engine.Reader().Events(t.Context(), telemetry.EventQuery{Fingerprint: issues.Items[0].Fingerprint, Page: orm.Page(1, 10)})
		if err != nil {
			t.Fatal(err)
		}
		if events.Total != 1 || len(events.Items) != 1 || !events.Items[0].Detail.Untrusted {
			t.Fatalf("browser event after synchronous flush: %+v", events)
		}
		if strings.Contains(strings.Join(app.PendingTasks(), ","), "provider[foundry.application.telemetry]") {
			t.Fatal("test helper started a telemetry loop")
		}
	}
}
