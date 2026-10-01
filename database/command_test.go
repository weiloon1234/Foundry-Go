package database_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func databaseCommand(t *testing.T, args ...string) dbcommand.Command {
	t.Helper()
	command, err := dbcommand.Parse(args, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func TestDatabaseCommandParsingNeedsNoServices(t *testing.T) {
	for _, args := range [][]string{nil, {"migrate"}, {"migrate", "down"}, {"migrate", "fresh"}, {"migrate", "up", "--force"}, {"migrate", "status", "--format", "csv"}, {"seed", "run", "positional"}, {"seed", "run", "--id", "invalid id"}, {"seed", "run", "--id", "app.a", "--id", "app.a"}, {"seed", "list", "--id", "app.a"}, {"migrate", "status", "--database", "bad name"}, {"migrate", "up", "--database", "main", "--database", "other"}, {"migrate", "up", "--schema", "tenant-a"}, {"migrate", "status", "--schema", "a", "--schema", "b"}, {"seed", "run", "--schema", "public"}, {"seed", "list", "--database", "main"}, {"migrate", "show"}, {"migrate", "show", "--migration", "app"}, {"migrate", "show", "--migration", "app/0001_first", "--migration", "app/0002_second"}, {"migrate", "show", "--migration", "App/0001"}} {
		if _, err := dbcommand.Parse(args, io.Discard); err == nil {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
	for _, args := range [][]string{{"--help"}, {"migrate", "up", "--help"}, {"seed", "run", "--help"}} {
		var help bytes.Buffer
		if _, err := dbcommand.Parse(args, &help); !errors.Is(err, flag.ErrHelp) || help.Len() == 0 {
			t.Fatalf("help: %q %v", help.String(), err)
		}
	}
	if err := (dbcommand.Command{}).Run(t.Context(), dbcommand.Resources{}, io.Discard); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero command accepted")
	}
	for _, args := range [][]string{{"migrate", "up"}, {"migrate", "status"}, {"seed", "run"}, {"seed", "list"}} {
		if err := databaseCommand(t, args...).Run(t.Context(), dbcommand.Resources{}, io.Discard); !errors.Is(err, fault.Missing) {
			t.Fatal("missing command resources accepted")
		}
	}
}

func TestMigrationCommandsReadStatusApplyAndExposeDrift(t *testing.T) {
	server := newMigrationServer()
	runner, _ := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	resources := dbcommand.Resources{Migrations: runner}
	var output bytes.Buffer
	status := databaseCommand(t, "migrate", "status", "--format", "json")
	if err := status.Run(t.Context(), resources, &output); err != nil {
		t.Fatal(err)
	}
	var report migrate.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || len(report.Statuses) != 2 || report.Statuses[0].State != migrate.Pending || server.table {
		t.Fatalf("read-only command: %s %v", output.Bytes(), err)
	}
	output.Reset()
	if err := databaseCommand(t, "migrate", "up").Run(t.Context(), resources, &output); err != nil || !strings.Contains(output.String(), "Confirmed 2 migration(s).") {
		t.Fatalf("migration command: %q %v", output.String(), err)
	}
	definitions := migrationDefinitions()
	definitions[0].SQL = []string{"different historical SQL"}
	changed, _ := migrationRunner(t, server, 2, definitions, nil)
	output.Reset()
	if err := status.Run(t.Context(), dbcommand.Resources{Migrations: changed}, &output); !errors.Is(err, fault.Conflict) {
		t.Fatalf("drift exit outcome: %v", err)
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || len(report.Problems) == 0 {
		t.Fatal("conflict status omitted the report")
	}
}

func TestMigrationCommandRetainsProgressOnUncertainCommit(t *testing.T) {
	server := newMigrationServer()
	server.loseCommit = true
	runner, _ := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	var output bytes.Buffer
	err := databaseCommand(t, "migrate", "up", "--format", "json").Run(t.Context(), dbcommand.Resources{Migrations: runner}, &output)
	if !errors.Is(err, database.CommitUnknown) {
		t.Fatalf("lost database outcome: %v", err)
	}
	var result migrate.RunResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Interrupted == nil || len(result.Applied) != 0 {
		t.Fatalf("uncertain progress report: %s %v", output.Bytes(), err)
	}
}

type failedCommandWriter struct{ err error }

func (w failedCommandWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCommandOutputFailureDoesNotRerunCommittedWork(t *testing.T) {
	server := newMigrationServer()
	runner, _ := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	cause := errors.New("closed output")
	err := databaseCommand(t, "migrate", "up").Run(t.Context(), dbcommand.Resources{Migrations: runner}, failedCommandWriter{cause})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "2 confirmed commits") || len(server.history) != 2 {
		t.Fatalf("output failure lost applied work: %v", err)
	}
	if len(server.committedSQL) != 2 {
		t.Fatal("output failure reran work")
	}
}

func TestSeederCommandsListWithoutDatabaseAndRunTypedSelection(t *testing.T) {
	var order []seed.ID
	definition := func(id seed.ID, requires ...seed.ID) seed.Definition {
		return seed.Definition{ID: id, Requires: requires, Run: func(context.Context, *database.Tx) error { order = append(order, id); return nil }}
	}
	registry, err := seed.New(definition("app.b", "app.a"), definition("app.a"), definition("app.other"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := databaseCommand(t, "seed", "list").Run(t.Context(), dbcommand.Resources{Seeders: registry}, &output); err != nil || output.String() != "app.a\napp.b\napp.other\n" {
		t.Fatalf("list: %q %v", output.String(), err)
	}
	db := open(t, &driverState{}, nil)
	resources := dbcommand.Resources{Database: db, Seeders: registry}
	output.Reset()
	if err := databaseCommand(t, "seed", "run", "--id", "app.b", "--format", "json").Run(t.Context(), resources, &output); err != nil {
		t.Fatal(err)
	}
	var result seed.Result
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !reflect.DeepEqual(order, []seed.ID{"app.a", "app.b"}) || !reflect.DeepEqual(result.Committed, order) {
		t.Fatalf("selected command: %s %v", output.Bytes(), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := databaseCommand(t, "seed", "run").Run(ctx, resources, io.Discard); !errors.Is(err, context.Canceled) || len(order) != 2 {
		t.Fatal("canceled command executed")
	}
}

func TestMigrationCommandsRunSchemaTargetsOfOneDatabase(t *testing.T) {
	public, tenant := newMigrationServer(), newMigrationServer()
	publicRunner, _ := migrationRunner(t, public, 1, migrationDefinitions(), nil)
	tenantRunner, _ := migrationRunner(t, tenant, 1, migrationDefinitions(), nil)
	resources := dbcommand.Resources{DatabaseName: "main", MigrationTargets: []dbcommand.MigrationTarget{{Schema: "tenant", Runner: tenantRunner}, {Schema: "public", Runner: publicRunner}}}
	var output bytes.Buffer
	if err := databaseCommand(t, "migrate", "status", "--format", "json").Run(t.Context(), resources, &output); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Database string `json:"database"`
		Targets  []struct {
			Schema     string           `json:"schema"`
			Migrations []migrate.Status `json:"migrations"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(output.Bytes(), &status); err != nil || status.Database != "main" || len(status.Targets) != 2 || status.Targets[0].Schema != "public" || status.Targets[1].Schema != "tenant" || len(status.Targets[1].Migrations) != 2 || public.table || tenant.table {
		t.Fatalf("read-only target status: %s %v", output.Bytes(), err)
	}
	output.Reset()
	if err := databaseCommand(t, "migrate", "up", "--database", "main", "--schema", "tenant").Run(t.Context(), resources, &output); err != nil || !strings.Contains(output.String(), "== main/tenant\nApplied: app\t0001_first") || strings.Contains(output.String(), "main/public") || len(tenant.history) != 2 || len(public.history) != 0 {
		t.Fatalf("schema selection: %q %v", output.String(), err)
	}
	output.Reset()
	if err := databaseCommand(t, "migrate", "up").Run(t.Context(), resources, &output); err != nil || !strings.Contains(output.String(), "== main/public\n") || !strings.Contains(output.String(), "Confirmed 0 migration(s).") || len(public.history) != 2 {
		t.Fatalf("every target of the database: %q %v", output.String(), err)
	}
	for name, test := range map[string]struct {
		args      []string
		resources dbcommand.Resources
		code      fault.Code
	}{
		"rollback without schema":      {[]string{"migrate", "rollback", "--confirm"}, resources, fault.Invalid},
		"unknown schema":               {[]string{"migrate", "status", "--schema", "absent"}, resources, fault.Missing},
		"other database":               {[]string{"migrate", "status", "--database", "other"}, resources, fault.Invalid},
		"runner with schema selection": {[]string{"migrate", "status", "--schema", "public"}, dbcommand.Resources{Migrations: publicRunner}, fault.Invalid},
		"runner with database name":    {[]string{"migrate", "status", "--database", "main"}, dbcommand.Resources{Migrations: publicRunner}, fault.Invalid},
		"runner and targets":           {[]string{"migrate", "status"}, dbcommand.Resources{Migrations: publicRunner, MigrationTargets: resources.MigrationTargets}, fault.Invalid},
		"repeated schema":              {[]string{"migrate", "status"}, dbcommand.Resources{MigrationTargets: []dbcommand.MigrationTarget{{Schema: "public", Runner: publicRunner}, {Schema: "public", Runner: tenantRunner}}}, fault.Duplicate},
	} {
		if err := databaseCommand(t, test.args...).Run(t.Context(), test.resources, io.Discard); !errors.Is(err, test.code) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(public.history) != 2 || len(tenant.history) != 2 {
		t.Fatal("refused selection changed history")
	}
	if databaseCommand(t, "seed", "run", "--database", "reports").Database() != "reports" || databaseCommand(t, "migrate", "status").Database() != "" {
		t.Fatal("database selection is not exposed to resource assembly")
	}
}

func TestMigrationShowPrintsOneDefinitionWithoutDatabaseIO(t *testing.T) {
	public, tenant := newMigrationServer(), newMigrationServer()
	publicRunner, _ := migrationRunner(t, public, 1, migrationDefinitions(), nil)
	tenantRunner, _ := migrationRunner(t, tenant, 1, migrationDefinitions()[:1], nil)
	show := databaseCommand(t, "migrate", "show", "--migration", "app/0002_second")
	if show.NeedsDatabase() || databaseCommand(t, "seed", "list").NeedsDatabase() || !databaseCommand(t, "migrate", "status").NeedsDatabase() {
		t.Fatal("database need is not reported for resource assembly")
	}
	var output bytes.Buffer
	if err := show.Run(t.Context(), dbcommand.Resources{Migrations: publicRunner}, &output); err != nil || !strings.Contains(output.String(), "Migration: app/0002_second\n") || !strings.Contains(output.String(), "Requires: app/0001_first\n-- SQL 1\nSECOND\n") {
		t.Fatalf("single runner show: %q %v", output.String(), err)
	}
	resources := dbcommand.Resources{DatabaseName: "main", MigrationTargets: []dbcommand.MigrationTarget{{Schema: "tenant", Runner: tenantRunner}, {Schema: "public", Runner: publicRunner}}}
	output.Reset()
	if err := databaseCommand(t, "migrate", "show", "--migration", "app/0001_first", "--format", "json").Run(t.Context(), resources, &output); err != nil {
		t.Fatal(err)
	}
	var shown struct {
		Targets []struct {
			Schema string      `json:"schema"`
			Key    migrate.Key `json:"key"`
			SQL    []string    `json:"sql"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(output.Bytes(), &shown); err != nil || len(shown.Targets) != 2 || shown.Targets[0].Schema != "public" || shown.Targets[1].SQL[0] != "FIRST" {
		t.Fatalf("target show: %s %v", output.Bytes(), err)
	}
	if err := databaseCommand(t, "migrate", "show", "--migration", "app/0002_second", "--schema", "tenant").Run(t.Context(), resources, io.Discard); !errors.Is(err, fault.Missing) {
		t.Fatal("unregistered target migration shown", err)
	}
	if len(public.ddl) != 0 || len(public.locks) != 0 || len(tenant.ddl) != 0 || len(tenant.locks) != 0 {
		t.Fatal("migrate show performed database work")
	}
}
