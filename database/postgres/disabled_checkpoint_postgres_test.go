package postgres_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresDisabledCheckpointKeepsRollbackChronological(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	config := migrate.DefaultPostgresConfig()
	config.Schema, config.SearchPath = schema, schema
	runner := func(definitions ...migrate.Definition) *migrate.Postgres {
		t.Helper()
		registry, err := migrate.New(definitions...)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := migrate.NewPostgres(db, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		return runner
	}
	feature := migrate.Definition{Key: migrate.Key{Origin: "foundry.test", ID: "001"}, Version: "v1", Mode: migrate.NonTransactional, SQL: []string{"CREATE TABLE checkpoint_data (id bigint PRIMARY KEY)", "INSERT INTO checkpoint_dependency VALUES (1)"}}
	initial := runner(feature)
	if _, err := initial.Up(t.Context()); !errors.Is(err, migrate.ErrReconciliationRequired) {
		t.Fatal("expected interrupted statement", err)
	}
	report, err := initial.Status(t.Context())
	if err != nil || report.Statuses[0].Progress == nil {
		t.Fatal("checkpoint missing", report, err)
	}
	if _, err := initial.Reconcile(t.Context(), *report.Statuses[0].Progress, migrate.StatementNotApplied); err != nil {
		t.Fatal(err)
	}
	feature.Availability = migrate.FeatureDisabled
	first := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "100"}, Version: "v1", SQL: []string{"CREATE TABLE earlier (id bigint PRIMARY KEY)"}, Down: []string{"DROP TABLE earlier"}}
	second := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "050"}, Version: "v1", SQL: []string{"CREATE TABLE later (id bigint PRIMARY KEY)"}, Down: []string{"DROP TABLE later"}}
	earlier, err := runner(feature, first).Up(t.Context())
	if err != nil || len(earlier.Applied) != 1 || earlier.Batch <= report.Statuses[0].Progress.Batch {
		t.Fatal("disabled checkpoint selected batch", earlier, err)
	}
	later, err := runner(feature, first, second).Up(t.Context())
	if err != nil || len(later.Applied) != 1 || later.Batch <= earlier.Batch {
		t.Fatal("batch reused", later, err)
	}
	if _, err := db.Exec(t.Context(), `CREATE TABLE "`+schema+`".checkpoint_dependency (id bigint PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	feature.Availability = migrate.Available
	recovered := runner(feature, first, second)
	if _, err := recovered.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	plan, err := recovered.RollbackPlan(t.Context(), 1)
	if err != nil || len(plan) != 1 || plan[0].Key != second.Key {
		t.Fatal("older migration selected for rollback", plan, err)
	}
	reversed, err := recovered.Rollback(t.Context(), 1)
	if err != nil || len(reversed.RolledBack) != 1 || reversed.RolledBack[0].Key != second.Key {
		t.Fatal("wrong rollback", reversed, err)
	}
	var count int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename='earlier'", []any{schema}, &count); err != nil || count != 1 {
		t.Fatal("older table was reverted", count, err)
	}
}
