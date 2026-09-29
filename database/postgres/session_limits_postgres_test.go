package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresSessionTimeoutsClassifyServerCancellationAndLockWaits(t *testing.T) {
	scope := pgtest.Isolate(t)
	config := scope.Config()
	config.StatementTimeout, config.LockTimeout = 500*time.Millisecond, 50*time.Millisecond
	db, err := postgres.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	if _, err := db.Exec(t.Context(), "SELECT pg_catalog.pg_sleep(2)"); !errors.Is(err, database.QueryCanceled) || errors.Is(err, database.Canceled) {
		t.Fatal("statement_timeout was not classified as a server cancellation", err)
	}
	execute(t, db, "CREATE TABLE locked_records(id bigint PRIMARY KEY)")
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), "LOCK TABLE locked_records IN ACCESS EXCLUSIVE MODE"); err != nil {
			return err
		}
		_, err := db.Exec(t.Context(), "INSERT INTO locked_records VALUES (1)")
		if !errors.Is(err, database.LockNotAvailable) {
			t.Error("lock_timeout was not classified as an unavailable lock", err)
		}
		return nil
	})
	if err != nil || countRows(t, db, "locked_records") != 0 || db.Stats().Owners != 0 {
		t.Fatal("lock wait failure changed data or ownership", err)
	}
}

func TestPostgresAutocommitStatementsReportServerRejection(t *testing.T) {
	db := pgtest.Isolate(t).Open(t)
	execute(t, db, "CREATE TABLE autocommit_records(id bigint PRIMARY KEY)")
	collect := func(statement string, args ...any) error {
		rows, err := db.FoundryAutocommitQuery(sqlowner.Seal{}, t.Context(), statement, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
		}
		return errors.Join(rows.Err(), rows.Close())
	}
	if err := collect("INSERT INTO autocommit_records VALUES ($1) RETURNING id", int64(1)); err != nil {
		t.Fatal(err)
	}
	for statement, code := range map[string]database.Code{
		"INSERT INTO autocommit_records VALUES (1) RETURNING id":   database.UniqueViolation,
		"INSERT INTO autocommit_records VALUES (1/0) RETURNING id": database.DivisionByZero,
		"INSERT INTO missing_records VALUES (1) RETURNING id":      database.QueryFailed,
	} {
		err := collect(statement)
		var detail *database.Error
		if !errors.Is(err, code) || !errors.As(err, &detail) || detail.Outcome() != database.RolledBack {
			t.Fatalf("%s: rejected statement lost its confirmed outcome: %v", statement, err)
		}
	}
	if countRows(t, db, "autocommit_records") != 1 || db.Stats().Owners != 0 {
		t.Fatal("autocommit statements changed unexpected rows or leaked ownership")
	}
}
