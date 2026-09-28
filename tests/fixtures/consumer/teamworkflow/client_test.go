package teamworkflow

import (
	"bytes"
	"foundry.test/consumer/internal/clientfixture"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/openapi"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"testing"
)

func TestGeneratedWorkflowClientAgainstCompleteApplication(t *testing.T) {
	tools := clientfixture.Load(t)
	app := startApp(t, pgtest.Isolate(t), pgtest.Isolate(t), Hooks{})
	router, err := foundation.Resolve(app.app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.HTTP) != 8 {
		t.Fatal("workflow transport surface changed")
	}
	for _, operation := range document.HTTP {
		switch operation.Route.ID {
		case "workflow.submit":
			if operation.Status != 202 || operation.Idempotency == nil || !operation.Preparation || operation.Route.Authentication == nil {
				t.Fatal("submission contract lost transaction/auth/preparation")
			}
		case "workflow.patch":
			if operation.Status != 200 || !operation.Preparation || operation.Route.Authentication == nil {
				t.Fatal("PATCH contract lost its handler lifecycle")
			}
		}
	}
	api, err := openapi.Render(source, openapi.Options{Title: "Workflow", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"discriminator"`, `"oneOf"`, `"Idempotency-Key"`, `"application/x-www-form-urlencoded"`, `"multipart/form-data"`, `"202"`} {
		if !bytes.Contains(api, []byte(required)) {
			t.Fatal("OpenAPI lost integrated contract", required)
		}
	}
	tools.Check(t, source, app.client.URL, "testdata")
	db, _ := app.app.Resources().Database()
	project, err := QueryWorkflowProjects().Where(ProjectFields().TeamID.Eq(1)).RequireFirst(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	title, _ := project.Title.Get()
	if title != "From TypeScript" || project.Budget != 42 {
		t.Fatal("generated client presence did not reach persisted drafts")
	}
	if scalar(t, db, `SELECT count(*) FROM workflow_submissions`) != 1 || scalar(t, db, `SELECT count(*) FROM foundry_outbox`) != 1 {
		t.Fatal("generated client duplicated a durable submission")
	}
	receiver, _ := app.app.Resources().Databases.Connection("receiver")
	waitFor(t, func() bool {
		return scalar(t, receiver, `SELECT total FROM workflow_delivery_totals WHERE name='submissions'`) == 1
	})
}
