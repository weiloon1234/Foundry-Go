package application_test

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type reportOperator struct{ ID int64 }

func (o reportOperator) FoundryReference() model.Reference[reportOperator, int64] {
	return model.NewReference[reportOperator]("report_operators", o.ID, codec.Signed[int64]())
}
func (o reportOperator) FoundryIdentity() (model.Identity, error) {
	return o.FoundryReference().Identity()
}

type operatorReport struct{ Owner int64 }

// Configured guards extend the application-owned registry, so permissions and
// policies contributed through ordinary assembly are usable on their routes.
// Before this, each guard had a private registry: WithPermissions failed at
// router assembly and Policy.Authorize returned Missing (HTTP 500).
func TestConfiguredGuardSharesApplicationAuthorization(t *testing.T) {
	migrationDB := pgtest.Open(t)
	schema := pgtest.Namespace(t, migrationDB)
	s := settings()
	s.HTTP.Enabled = false
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	s.Features.Auth.Tokens.Enabled = true
	s.Features.Auth.Tokens.Schema = schema
	viewReports := auth.DefinePermission("reports.view", func(_ context.Context, operator reportOperator) (bool, error) {
		return operator.ID != 9, nil
	})
	readReport := auth.DefinePolicy("reports.read", func(_ context.Context, operator reportOperator, report operatorReport) (bool, error) {
		return operator.ID == report.Owner, nil
	})
	app, err := application.New(s, quiet()).Register(application.Authorization("fixture.authorization",
		application.Authorize(viewReports), application.Authorize(readReport),
	)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := migrationDB.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, target := range app.Migrations() {
			for _, definition := range target.Definitions {
				for _, sql := range definition.SQL {
					if _, err := tx.Exec(t.Context(), sql); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services := app.Resources()
	registry, err := services.Authorization()
	if err != nil {
		t.Fatal(err)
	}
	if err := viewReports.ValidateIn(registry); err != nil {
		t.Fatal("contributed permission missing from the application registry", err)
	}
	provider := auth.DefineProvider("fixture.operators", (reportOperator{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[reportOperator], error) {
		return value.Set(reportOperator{ID: id}), nil
	}, func(context.Context, reportOperator) (bool, error) { return true, nil })
	allowed, err := auth.NewAccessScopes[reportOperator]()
	if err != nil {
		t.Fatal(err)
	}
	operators, err := application.NewTokenGuard(services, "", provider, "operator.bearer", allowed)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "fixture.reports", Method: http.GET, Access: http.Guarded}, http.StaticPath("/reports")), http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
	route := http.Authenticated(endpoint, operators.Binding).WithPermissions(viewReports).Handle(func(ctx context.Context, operator reportOperator, _ http.Input[http.NoPath, http.NoQuery, http.NoBody]) (http.NoContent, error) {
		return http.NoContent{}, readReport.Authorize(ctx, operators.Tokens.Guard(), operatorReport{Owner: 7})
	})
	router, err := http.NewRouter(route)
	if err != nil {
		t.Fatal("configured guard could not use a contributed permission", err)
	}
	serve := func(id int64) int {
		proof, err := auth.NewProof(reportOperator{ID: id}.FoundryReference(), auth.Authenticated)
		if err != nil {
			t.Fatal(err)
		}
		issued, err := operators.Tokens.Issue(t.Context(), proof, token.IssueOptions[reportOperator]{Name: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", "http://fixture.test/reports", nil)
		request.Header.Set("Authorization", "Bearer "+issued.AccessSecret().Reveal())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder.Code
	}
	for id, expected := range map[int64]int{7: stdhttp.StatusNoContent, 8: stdhttp.StatusForbidden, 9: stdhttp.StatusForbidden} {
		if code := serve(id); code != expected {
			t.Fatalf("operator %d status %d, want %d", id, code, expected)
		}
	}
}
