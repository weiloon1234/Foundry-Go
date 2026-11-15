package application_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	pgapp "github.com/weiloon1234/Foundry-Go/testkit/postgres/application"
)

func TestStartRetainsDisabledOnlyHistoryAndRepeatedDomainDeclarations(t *testing.T) {
	mainScope, auditScope := pgtest.Isolate(t), pgtest.Isolate(t)
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.Services.Database.Connections = infrastructure.DatabaseConnections{
		"default": infrastructure.DefaultConnectionSettings(),
		"audit":   infrastructure.DefaultConnectionSettings(),
	}
	s.Features.Audit.Enabled, s.Features.Audit.Database = true, "audit"
	environment, err := pgapp.Bind(s.Services.Database, pgapp.On(mainScope, "default"), pgapp.On(auditScope, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.WithDatabaseScopes(environment.Settings())
	if err != nil {
		t.Fatal(err)
	}
	build := func(targets ...infrastructure.MigrationTarget) *application.App {
		t.Helper()
		app, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Migrations(targets...).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	stop := func(app *application.App) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}
	enabled := build()
	if err := environment.Start(t, enabled); err != nil {
		t.Fatal(err)
	}
	stop(enabled)
	db := auditScope.Open(t)
	var auditCount int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM schema_migrations", nil, &auditCount); err != nil || auditCount == 0 {
		t.Fatal("audit history not applied", auditCount, err)
	}

	a := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "a"}, Version: "v1", SQL: []string{"CREATE TABLE first_domain (id bigint PRIMARY KEY)"}}
	b := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "b"}, Version: "v1", SQL: []string{"CREATE TABLE second_domain (id bigint PRIMARY KEY)"}}
	dependent := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "c"}, Version: "v1", SQL: []string{"INSERT INTO first_domain VALUES (1)"}, Requires: []migrate.Key{b.Key, a.Key}}
	domain := infrastructure.MigrationTarget{Connection: "audit", Definitions: []migrate.Definition{a, b, dependent}}
	s.Features.Audit.Enabled = false
	disabled := build()
	for _, target := range disabled.Migrations() {
		if target.Schema == auditScope.Schema() {
			t.Fatal("regression requires audit to be a disabled-only target")
		}
	}
	if err := environment.Start(t, disabled, domain); err != nil {
		t.Fatal("disabled-only history blocked an unrelated migration", err)
	}
	stop(disabled)

	// Build canonicalizes [B, A] into [A, B]. Start must accept the original
	// domain declaration again, as existing fixtures routinely supply it twice.
	repeated := build(domain)
	if err := environment.Start(t, repeated, domain); err != nil {
		t.Fatal("original declaration conflicted with its runnable snapshot", err)
	}
	stop(repeated)
	var count int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM schema_migrations", nil, &count); err != nil || count != auditCount+3 {
		t.Fatal("history was lost or repeated", count, err)
	}
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM first_domain", nil, &count); err != nil || count != 1 {
		t.Fatal("domain migration did not apply exactly once", count, err)
	}
	for _, group := range repeated.MigrationHistory() {
		if group.Schema != auditScope.Schema() {
			continue
		}
		registry, err := migrate.New(group.Definitions...)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := migrate.NewPostgres(db, registry, group.PostgresConfig(migrate.DefaultPostgresConfig()))
		if err != nil {
			t.Fatal(err)
		}
		report, err := runner.Status(t.Context())
		if err != nil || report.Check() != nil {
			t.Fatal("complete history status failed", report, err)
		}
		for _, status := range report.Statuses {
			if status.State != migrate.Complete {
				t.Fatal("migration did not remain applied", status)
			}
		}
	}
}
