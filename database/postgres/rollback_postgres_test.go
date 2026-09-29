package postgres_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	dbcommand "github.com/weiloon1234/Foundry-Go/database/command"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresRollbackReversesDeclaredDownMigrations(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	table := `"` + schema + `".rollback_records`
	key := func(id migrate.ID) migrate.Key { return migrate.Key{Origin: "app", ID: id} }
	definitions := []migrate.Definition{
		{Key: key("001_create"), Version: "v1", SQL: []string{"CREATE TABLE " + table + " (id bigint PRIMARY KEY)"}, Down: []string{"DROP TABLE " + table}},
		{Key: key("002_label"), Version: "v1", Requires: []migrate.Key{key("001_create")}, SQL: []string{"ALTER TABLE " + table + " ADD COLUMN label text"}, Down: []string{"ALTER TABLE " + table + " DROP COLUMN label"}},
		{Key: key("003_seed"), Version: "v1", SQL: []string{"INSERT INTO " + table + " (id) VALUES (1)"}},
	}
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	// Down is not part of the checksum: adding it never drifts history.
	withoutDown := definitions[0]
	withoutDown.Down = nil
	plain, err := migrate.New(withoutDown)
	if err != nil || plain.Entries()[0].Checksum != registry.Entries()[0].Checksum || plain.Entries()[0].Reversible || !registry.Entries()[0].Reversible {
		t.Fatal("Down changed the checksum or reversibility metadata", err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema = schema
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The newest migration has no Down, so rollback refuses before changing anything.
	if _, err := runner.Rollback(t.Context(), 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("irreversible migration rolled back", err)
	}
	var rows int64
	if err := scanCount(t, db, "SELECT count(*) FROM "+table, &rows); err != nil || rows != 1 {
		t.Fatal("refused rollback changed data", rows, err)
	}

	// Without --confirm the command only prints the plan.
	resolved, err := migrate.New(append(definitions[:2:2], migrate.Definition{Key: key("003_seed"), Version: "v1", SQL: definitions[2].SQL, Down: []string{"DELETE FROM " + table + " WHERE id = 1"}})...)
	if err != nil {
		t.Fatal(err)
	}
	runner, err = migrate.NewPostgres(db, resolved, config)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	plan, err := dbcommand.Parse([]string{"migrate", "rollback", "--step", "2"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Run(t.Context(), dbcommand.Resources{Migrations: runner}, &output); !errors.Is(err, fault.Invalid) || !strings.Contains(output.String(), "Would roll back: app\t003_seed") || !strings.Contains(output.String(), "002_label") {
		t.Fatal("unconfirmed rollback did not stop at the plan", output.String(), err)
	}
	if report, err := runner.Status(t.Context()); err != nil || report.Statuses[2].State != migrate.Complete {
		t.Fatal("planning changed history", err)
	}
	if planned, err := runner.RollbackPlan(t.Context(), 3); err != nil || len(planned) != 3 || planned[0].Key != key("003_seed") || planned[2].Key != key("001_create") {
		t.Fatal("rollback plan is not newest first", planned, err)
	}
	confirmed, err := dbcommand.Parse([]string{"migrate", "rollback", "--step", "2", "--confirm"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := confirmed.Run(t.Context(), dbcommand.Resources{Migrations: runner}, &output); err != nil || !strings.Contains(output.String(), "Confirmed 2 rollback(s).") {
		t.Fatal("confirmed rollback", output.String(), err)
	}
	report, err := runner.Status(t.Context())
	if err != nil || report.Statuses[0].State != migrate.Complete || report.Statuses[1].State != migrate.Pending || report.Statuses[2].State != migrate.Pending {
		t.Fatal("rollback history", report, err)
	}
	if err := scanCount(t, db, "SELECT count(*) FROM information_schema.columns WHERE table_schema = '"+schema+"' AND table_name = 'rollback_records' AND column_name = 'label'", &rows); err != nil || rows != 0 {
		t.Fatal("Down SQL did not run", rows, err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal("reapplying rolled-back migrations", err)
	}
	if _, err := dbcommand.Parse([]string{"migrate", "rollback", "--step", "0"}, &output); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero steps accepted", err)
	}
	if _, err := migrate.New(migrate.Definition{Key: key("004"), Version: "v1", Mode: migrate.NonTransactional, SQL: []string{"SELECT 1"}, Down: []string{"SELECT 1"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("nontransactional Down accepted", err)
	}
}

func scanCount(t *testing.T, db database.Executor, statement string, into *int64) error {
	t.Helper()
	return database.ScanOne(t.Context(), db, statement, nil, into)
}
