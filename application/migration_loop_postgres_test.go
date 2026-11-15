package application_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestAppMigrationsLegacyLoopRetainsDisabledFeatureHistory(t *testing.T) {
	schema := pgtest.Namespace(t, pgtest.Open(t))
	schemaDB := pgtest.Open(t, func(c *postgres.Config) { c.Schema = schema })
	s := migrationSettings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection, "alias": connection}
	s.Features.Audit.Enabled, s.Features.Audit.Schema = true, schema
	s.Features.OutboundCapture.Schema = schema
	build := func(domain ...infrastructure.MigrationTarget) *application.App {
		t.Helper()
		app, err := application.New(s, quiet()).Migrations(domain...).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(t, app) })
		return app
	}
	run := func(app *application.App) []migrate.Applied {
		t.Helper()
		var applied []migrate.Applied
		// Keep the consumer's original loop: no grouping helper or registry option.
		for _, target := range app.Migrations() {
			for _, definition := range target.Definitions {
				if definition.Availability != migrate.Available {
					t.Fatal("legacy SQL loop received a disabled definition")
				}
			}
			registry, err := migrate.New(target.Definitions...)
			if err != nil {
				t.Fatal(err)
			}
			config := migrate.DefaultPostgresConfig()
			config.Schema = schema
			runner, err := migrate.NewPostgres(schemaDB, registry, config)
			if err != nil {
				t.Fatal(err)
			}
			report, err := runner.Status(t.Context())
			if err != nil || report.Check() != nil {
				t.Fatal("legacy status", report, err)
			}
			result, err := runner.Up(t.Context())
			if err != nil {
				t.Fatal("legacy migration loop", err)
			}
			applied = append(applied, result.Applied...)
		}
		return applied
	}
	if applied := run(build()); len(applied) == 0 {
		t.Fatal("feature never migrated")
	}
	s.Features.Audit.Enabled = false
	unrelated := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "999999_unrelated"}, Version: "v1", SQL: []string{"CREATE TABLE loop_unrelated (id bigint PRIMARY KEY)"}}
	disabled := build(infrastructure.MigrationTarget{Connection: "alias", Schema: schema, Definitions: []migrate.Definition{unrelated}})
	applied := run(disabled)
	if len(applied) != 1 || applied[0].Key != unrelated.Key {
		t.Fatal("disabled history blocked unrelated migration", applied)
	}
	if applied := run(disabled); len(applied) != 0 {
		t.Fatal("migration repeated", applied)
	}
	// The command path uses the same immutable registries, including disabled-only targets.
	for _, name := range []string{"default", "alias"} {
		command, err := dbcommand.Parse([]string{"migrate", "status", "--database", name, "--schema", schema}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := disabled.RunDatabaseCommand(t.Context(), command, application.DatabaseCommandResources{}, &output); err != nil || !strings.Contains(output.String(), "foundry.audit") {
			t.Fatal("command lost disabled history or database alias", output.String(), err)
		}
	}
}
