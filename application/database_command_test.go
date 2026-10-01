package application_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestRunDatabaseCommandMigratesEverySchemaOfOnlyTheSelectedDatabase(t *testing.T) {
	db := pgtest.Open(t)
	schema, tenant := pgtest.Namespace(t, db), pgtest.Namespace(t, db)
	s := settings()
	s.HTTP.Enabled = false
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	connection.Primary.Schema = schema
	// The command opens only the selected pool, so this endpoint is never dialed.
	elsewhere := connection
	elsewhere.Primary.Host = "unreachable.invalid"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection, "elsewhere": elsewhere}
	records := func(sql string) []migrate.Definition {
		return []migrate.Definition{{Key: migrate.Key{Origin: "app", ID: "000001_records"}, Version: "v1", SQL: []string{sql}}}
	}
	app, err := application.New(s, quiet()).Migrations(
		infrastructure.MigrationTarget{Definitions: records("CREATE TABLE records (id bigint PRIMARY KEY)")},
		infrastructure.MigrationTarget{Schema: tenant, Definitions: records("CREATE TABLE records (id bigint PRIMARY KEY, tenant text NOT NULL)")},
		infrastructure.MigrationTarget{Connection: "elsewhere", Definitions: records("CREATE TABLE remote_records (id bigint PRIMARY KEY)")},
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	run := func(args ...string) (string, error) {
		command, err := dbcommand.Parse(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		err = app.RunDatabaseCommand(t.Context(), command, application.DatabaseCommandResources{}, &output)
		return output.String(), err
	}
	count := func(statement string, args ...any) int64 {
		var n int64
		if err := database.ScanOne(t.Context(), db, statement, args, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	historyTables := func() int64 {
		return count("SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname IN ($1, $2) AND tablename = 'schema_migrations'", schema, tenant)
	}
	output, err := run("migrate", "status")
	if err != nil || !strings.Contains(output, "== default/"+schema+"\n") || !strings.Contains(output, "== default/"+tenant+"\n") || historyTables() != 0 {
		t.Fatalf("read-only status: %q %v", output, err)
	}
	if output, err = run("migrate", "up"); err != nil || strings.Count(output, "Confirmed 1 migration(s).") != 2 {
		t.Fatalf("migrate every schema of the default database: %q %v", output, err)
	}
	// Each schema keeps its own history beside its own unqualified objects.
	if count(`SELECT count(*) FROM "`+schema+`".schema_migrations`)+count(`SELECT count(*) FROM "`+tenant+`".schema_migrations`) != 2 || count(`SELECT count(*) FROM "`+tenant+`".records WHERE tenant <> ''`) != 0 {
		t.Fatal("schema targets did not keep separate objects and history")
	}
	if _, err := run("migrate", "rollback"); !errors.Is(err, fault.Invalid) {
		t.Fatal("rollback across several schemas did not require --schema", err)
	}
	if output, err = run("migrate", "status", "--schema", tenant, "--format", "json"); err != nil || !strings.Contains(output, `"schema": "`+tenant+`"`) || strings.Contains(output, `"schema": "`+schema+`"`) {
		t.Fatalf("schema selection: %q %v", output, err)
	}
	if _, err := run("migrate", "status", "--database", "absent"); !errors.Is(err, fault.Missing) {
		t.Fatal("unknown database selection accepted", err)
	}
}

func TestRunDatabaseCommandSeedsWithTheApplicationKeyRing(t *testing.T) {
	db := pgtest.Open(t)
	s := settings()
	s.HTTP.Enabled = false
	s.Encryption.KeyID, s.Encryption.Key = "app_2026", generatedKey(t, "app_2026").Secret()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	connection.Primary.Schema = pgtest.Namespace(t, db)
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	// Seeders that write encrypted model fields seal with the transaction's key ring.
	keyed := false
	seeders, err := seed.New(seed.Definition{ID: "app.keyed", Run: func(_ context.Context, tx *database.Tx) error {
		keyed = tx.Encryption() != nil
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	command, err := dbcommand.Parse([]string{"seed", "run"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RunDatabaseCommand(t.Context(), command, application.DatabaseCommandResources{Seeders: seeders}, io.Discard); err != nil || !keyed {
		t.Fatal("database command pool lacks the application key ring", err)
	}
}
