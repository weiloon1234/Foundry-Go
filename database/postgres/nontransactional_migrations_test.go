package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresConcurrentMigrationCheckpointAndRecovery(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	table := `"` + schema + `".online_records`
	gate := `"` + schema + `".resume_gate`
	execute(t, db, "CREATE TABLE "+table+" (id bigint PRIMARY KEY, label text)")
	execute(t, db, "CREATE TABLE "+gate+" (ready boolean NOT NULL)")
	registry, err := migrate.New(migrate.Definition{Key: migrate.Key{Origin: "app", ID: "online_index"}, Version: "v1", Mode: migrate.NonTransactional, SQL: []string{
		"CREATE INDEX CONCURRENTLY online_label_idx ON " + table + " (label)",
		"SELECT pg_sleep(CASE WHEN EXISTS(SELECT 1 FROM " + gate + ") THEN 0 ELSE 30 END)",
	}})
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema = schema
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	before, err := runner.Status(t.Context())
	if err != nil || before.Statuses[0].State != migrate.Pending {
		t.Fatal(err)
	}
	operation, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := runner.Up(operation); finished <- err }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		report, err := runner.Status(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		progress := report.Statuses[0].Progress
		if progress != nil && progress.Confirmed == 1 && progress.State == migrate.StepInFlight {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("migration did not reach its journaled wait")
		}
		select {
		case err := <-finished:
			t.Fatal("migration exited before cancellation", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled statement succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled migration did not release session")
	}
	var valid bool
	err = database.ScanOne(t.Context(), db, "SELECT i.indisvalid FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid=i.indexrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='online_label_idx'", []any{schema}, &valid)
	if err != nil || !valid {
		t.Fatal("concurrent index did not commit outside a transaction", err)
	}
	report, err := runner.Status(t.Context())
	if err != nil || report.Check() == nil || report.Statuses[0].Progress == nil {
		t.Fatal("interrupted progress absent", err)
	}
	interrupted := *report.Statuses[0].Progress
	if _, err := runner.Up(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal("uncertain command was retried", err)
	}
	// The canceled statement was only a wait. Attest that no durable effect remains;
	// enable its successful retry without editing the immutable migration definition.
	execute(t, db, "INSERT INTO "+gate+" (ready) VALUES(true)")
	ready, err := runner.Reconcile(t.Context(), interrupted, migrate.StatementNotApplied)
	if err != nil || ready.Confirmed != 1 {
		t.Fatal(ready, err)
	}
	result, err := runner.Up(t.Context())
	if err != nil || len(result.Applied) != 1 {
		t.Fatal("resume repeated existing index or failed", result, err)
	}
	report, err = runner.Status(t.Context())
	if err != nil || report.Check() != nil || report.Statuses[0].State != migrate.Complete || report.Statuses[0].Progress != nil {
		t.Fatal("final history/progress inconsistent", err)
	}
	if result, err = runner.Up(t.Context()); err != nil || len(result.Applied) != 0 {
		t.Fatal("completed nontransactional migration repeated", err)
	}
}
