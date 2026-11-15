package application_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
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
	// Framework feature targets come first; a database brings outbound capture.
	got := app.Migrations()
	frameworkCapture := false
	for _, target := range got[:len(got)-1] {
		frameworkCapture = frameworkCapture || slices.ContainsFunc(target.Definitions, func(d migrate.Definition) bool {
			return d.Key.Origin == outboundcapture.MigrationOrigin && d.Availability == migrate.Available
		})
	}
	domain := &got[len(got)-1]
	if !frameworkCapture || domain.Connection != "" || domain.Definitions[0].SQL[0] != "CREATE TABLE records (id uuid PRIMARY KEY)" {
		t.Fatalf("application migrations: %+v", got)
	}
	domain.Definitions[0].SQL[0] = "changed after inspection"
	if app.Migrations()[len(got)-1].Definitions[0].SQL[0] != "CREATE TABLE records (id uuid PRIMARY KEY)" {
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
