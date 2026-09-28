package configuredprofile

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

type auditPayload struct {
	Label string `json:"label"`
}

func TestAuditScopeSurvivesConstructorAndKeepsBusinessTransaction(t *testing.T) {
	migrationDB := pgtest.Open(t)
	schema := pgtest.Namespace(t, migrationDB)
	s := Defaults()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection, "other": connection}
	s.Features.Audit.Enabled = true
	s.Features.Audit.Schema = schema
	action := audit.Define[auditPayload]("constructed.audit", 1)
	var retained *audit.Scope
	var recorded audit.ActionID[auditPayload]
	failures := make(chan error, 1)
	app, err := application.New(s, quiet()).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
		scope, err := services.AuditScope()
		if err != nil {
			return nil, err
		}
		retained = scope
		db, err := services.Database()
		if err != nil {
			return nil, err
		}
		route := http.DefineRoute(http.RouteSpec{ID: "profile.audit", Method: http.POST, Access: http.Public}, http.StaticPath("/audit"))
		return []http.RouteRegistration{route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ http.NoPath) {
			err := db.Transaction(r.Context(), func(tx *database.Tx) error {
				return scope.Within(r.Context(), tx, func(child *database.Tx, recorder *audit.Recorder) error {
					var err error
					recorded, err = action.Record(r.Context(), child, recorder, auditPayload{"after construction"})
					return err
				})
			})
			if err != nil {
				failures <- err
				w.WriteHeader(500)
				return
			}
			w.WriteHeader(201)
		})}, nil
	}).Build(t.Context())
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
				for _, statement := range definition.SQL {
					if _, err := tx.Exec(t.Context(), statement); err != nil {
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
	scope, err := app.Resources().AuditScope()
	if err != nil || scope != retained {
		t.Fatal("constructor/runtime audit scope identity differs", err)
	}
	router, err := foundation.Resolve(app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodPost, "/audit", nil))
	select {
	case err := <-failures:
		t.Fatal("retained dependency failed after constructor sealed", err)
	default:
	}
	if response.Code != 201 || recorded.IsZero() {
		t.Fatal("constructed handler did not record")
	}
	db, err := app.Resources().Database()
	if err != nil {
		t.Fatal(err)
	}
	find := func(id audit.ActionID[auditPayload]) bool {
		t.Helper()
		var found bool
		if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			var before, after string
			if err := database.ScanOne(t.Context(), tx, "SHOW search_path", nil, &before); err != nil {
				return err
			}
			if err := scope.Within(t.Context(), tx, func(child *database.Tx, recorder *audit.Recorder) error {
				item, err := action.Find(t.Context(), child, recorder, id)
				found = item.IsSet()
				return err
			}); err != nil {
				return err
			}
			if err := database.ScanOne(t.Context(), tx, "SHOW search_path", nil, &after); err != nil {
				return err
			}
			if before != after {
				return errors.New("audit scope leaked search_path")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return found
	}
	if !find(recorded) {
		t.Fatal("committed audit missing")
	}
	rollback := errors.New("business rollback")
	var discarded audit.ActionID[auditPayload]
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := scope.Within(t.Context(), tx, func(child *database.Tx, recorder *audit.Recorder) error {
			var err error
			discarded, err = action.Record(t.Context(), child, recorder, auditPayload{"rolled back"})
			return err
		}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || find(discarded) {
		t.Fatal("scope escaped business rollback", err)
	}
	other, err := app.Resources().Databases.Connection("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Transaction(t.Context(), func(tx *database.Tx) error {
		return scope.Within(t.Context(), tx, func(*database.Tx, *audit.Recorder) error { return nil })
	}); err == nil {
		t.Fatal("scope accepted another pool")
	}
	if err := scope.Within(context.Background(), nil, func(*database.Tx, *audit.Recorder) error { return nil }); err == nil {
		t.Fatal("scope accepted no transaction")
	}
}
