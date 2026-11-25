package tooling

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	telemetrytest "github.com/weiloon1234/Foundry-Go/testkit/telemetry"
)

func assertBrowserIssue(t *testing.T, app *application.App) {
	// doc-example:begin persisted-telemetry-3
	engine, err := app.Resources().Telemetry()
	if err != nil {
		t.Fatal(err)
	}
	// Exercise browser admission and wait for any asynchronous error reporters.
	telemetrytest.Flush(t, engine)
	issues, err := engine.Reader().Issues(t.Context(), telemetry.IssueQuery{
		Source: telemetry.SourceBrowser,
		Page:   orm.Page(1, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if issues.Total != 1 {
		t.Fatalf("browser issues = %d, want 1", issues.Total)
	}
	// doc-example:end persisted-telemetry-3
}
