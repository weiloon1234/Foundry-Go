package application_test

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	outboundcapture "github.com/weiloon1234/Foundry-Go/httpclient/capture"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func migrationSettings() application.Settings {
	s := settings()
	s.HTTP.Enabled = false
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary.Host, connection.Primary.Database, connection.Primary.User = "db.internal", "app", "app"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	return s
}

func recordsMigration(sql string) migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "000001_records"}, Version: "v1", SQL: []string{sql}}
}

func TestBuilderMigrationsJoinApplicationTargetsAsSnapshots(t *testing.T) {
	targets := []infrastructure.MigrationTarget{{Definitions: []migrate.Definition{recordsMigration("CREATE TABLE records (id uuid PRIMARY KEY)")}}}
	app, err := application.New(migrationSettings(), quiet()).Migrations(targets...).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	targets[0].Definitions[0].SQL[0] = "changed after registration"
	got := app.Migrations()
	if len(got) != 1 || got[0].Connection != "default" {
		t.Fatalf("physical migration targets: %+v", got)
	}
	definitions := got[0].Definitions
	if !slices.ContainsFunc(definitions, func(d migrate.Definition) bool { return d.Key.Origin == outboundcapture.MigrationOrigin }) {
		t.Fatal("framework capture migration missing")
	}
	index := slices.IndexFunc(definitions, func(d migrate.Definition) bool { return d.Key == targets[0].Definitions[0].Key })
	if index < 0 || definitions[index].SQL[0] != "CREATE TABLE records (id uuid PRIMARY KEY)" {
		t.Fatal("application declaration changed")
	}
	definitions[index].SQL[0] = "changed after inspection"
	if app.Migrations()[0].Definitions[index].SQL[0] != "CREATE TABLE records (id uuid PRIMARY KEY)" {
		t.Fatal("App.Migrations exposed owned definitions")
	}
}

func TestBuilderMigrationsAreValidatedAtBuild(t *testing.T) {
	for name, test := range map[string]struct {
		targets []infrastructure.MigrationTarget
		code    fault.Code
	}{
		"conflicting definitions": {[]infrastructure.MigrationTarget{{Definitions: []migrate.Definition{recordsMigration("CREATE TABLE records (id uuid PRIMARY KEY)")}}, {Schema: "public", Definitions: []migrate.Definition{recordsMigration("CREATE TABLE records (id bigint PRIMARY KEY)")}}}, fault.Conflict},
		"unknown connection":      {[]infrastructure.MigrationTarget{{Connection: "absent", Definitions: []migrate.Definition{recordsMigration("SELECT 1")}}}, fault.Missing},
		"invalid schema":          {[]infrastructure.MigrationTarget{{Schema: "tenant-a", Definitions: []migrate.Definition{recordsMigration("SELECT 1")}}}, fault.Invalid},
	} {
		if _, err := application.New(migrationSettings(), quiet()).Migrations(test.targets...).Build(t.Context()); !errors.Is(err, test.code) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestMigrationHistoryIncludesDisabledOnlyTargetsAsOwnedSnapshots(t *testing.T) {
	s := migrationSettings()
	s.Features.Audit.Schema = "historical_audit"
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	for _, target := range app.Migrations() {
		if target.Schema == s.Features.Audit.Schema {
			t.Fatal("disabled-only target exposed to legacy runner loop")
		}
	}
	groups := app.MigrationHistory()
	index := slices.IndexFunc(groups, func(group infrastructure.MigrationGroup) bool { return group.Schema == s.Features.Audit.Schema })
	if index < 0 {
		t.Fatal("disabled-only history disappeared")
	}
	registry, err := migrate.New(groups[index].Definitions...)
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.Inspect(nil)
	if err != nil || report.Check() != nil || len(report.Statuses) == 0 {
		t.Fatal("disabled-only inspection failed", report, err)
	}
	for _, status := range report.Statuses {
		if status.State != migrate.PendingDisabled {
			t.Fatal("disabled-only migration became runnable", status)
		}
	}
	original := groups[index].Definitions[0].SQL[0]
	groups[index].Definitions[0].SQL[0], groups[index].Connections[0] = "mutated", "mutated"
	again := app.MigrationHistory()[index]
	if again.Definitions[0].SQL[0] != original || again.Connections[0] != "default" {
		t.Fatal("history snapshots share mutable storage")
	}
	show, err := dbcommand.Parse([]string{"migrate", "show", "--schema", s.Features.Audit.Schema, "--migration", string(again.Definitions[0].Key.Origin) + "/" + string(again.Definitions[0].Key.ID)}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := app.RunDatabaseCommand(t.Context(), show, application.DatabaseCommandResources{}, &output); err != nil || !strings.Contains(output.String(), "foundry.audit") {
		t.Fatal("command cannot inspect disabled-only history", output.String(), err)
	}
}
