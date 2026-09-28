package pluginusage_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/pluginusage"
	"foundry.test/pluginbase"
	"foundry.test/plugindep"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestIndependentPluginConsumerRunsRoutesEventsJobsAndDistributions(t *testing.T) {
	builder, err := pluginusage.Builder("application")
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := builder.Inspect(t.Context())
	if err != nil || len(inspection.Plugins) != 2 || inspection.Plugins[0].ID != pluginbase.ID || inspection.Plugins[1].ID != plugindep.ID {
		t.Fatal("plugin inspection order", inspection, err)
	}
	app := testkit.Start(t, builder)
	trace, err := foundation.Resolve(app.Services(), pluginbase.Journal)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(trace.Entries(), []string{"base.boot", "reports.boot"}) {
		t.Fatal("plugin boot order", trace.Entries())
	}
	router, err := foundation.Resolve(app.Services(), pluginbase.Router)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/reports", nil))
	if response.Code != 200 || response.Body.String() != "application reports" {
		t.Fatal("plugin app configuration", response.Result())
	}
	bus, err := foundation.Resolve(app.Services(), pluginbase.Bus)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugindep.Changed.Dispatch(t.Context(), bus, plugindep.Payload{Text: "changed"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(trace.Entries(), "event:changed") {
		t.Fatal("plugin event listener did not run")
	}
	bundles, err := pluginusage.Bundles(app.Services())
	if err != nil || len(bundles) != 1 {
		t.Fatal(bundles, err)
	}
	assetRoot := t.TempDir()
	if _, err := bundles[0].Publish(t.Context(), assetRoot, false); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(filepath.Join(assetRoot, "css/reports.css")); err != nil || !strings.Contains(string(contents), "display: grid") {
		t.Fatal("plugin asset", err)
	}
	scaffold, err := pluginusage.ReportScaffold(app.Services())
	if err != nil {
		t.Fatal(err)
	}
	scaffoldRoot := t.TempDir()
	if _, err := scaffold.Publish(t.Context(), scaffoldRoot, plugindep.ScaffoldInput{TypeName: "Monthly"}, false); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(filepath.Join(scaffoldRoot, "report.go")); err != nil || !strings.Contains(string(contents), "type Monthly struct") {
		t.Fatal("typed plugin scaffold", err)
	}
	dispatcher, err := foundation.Resolve(app.Services(), pluginbase.Dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plugindep.Job.Dispatch(t.Context(), dispatcher, plugindep.Payload{Text: "rendered"}, jobs.Options[plugindep.Payload]{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.Worker) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-app.Done():
		case <-time.After(app.ShutdownTimeout()):
			t.Error("plugin worker did not stop")
		}
	})
	if err := trace.Wait(ctx, "job:rendered"); err != nil {
		t.Fatal("plugin job handler did not run", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(app.ShutdownTimeout()):
		t.Fatal("worker run did not return")
	}
	entries := trace.Entries()
	if len(entries) < 2 || !slices.Equal(entries[len(entries)-2:], []string{"reports.shutdown", "base.shutdown"}) {
		t.Fatal("plugin shutdown order", entries)
	}
}
