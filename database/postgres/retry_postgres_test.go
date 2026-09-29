package postgres_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// PostgreSQL usually reports serialization failures and deadlocks on a
// statement inside the transaction, not at COMMIT. Retry must re-run exactly
// those confirmed rollbacks and leave no effect of the failed attempt.
func TestPostgresRetryRerunsInStatementSerializationFailuresAndDeadlocks(t *testing.T) {
	db := pgtest.Isolate(t).Open(t)
	execute(t, db, "CREATE TABLE retry_records(id bigint PRIMARY KEY)")
	policy := database.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
	for index, test := range []struct {
		state    string
		code     database.Code
		attempts int64
	}{
		{"40001", database.SerializationFailure, 2},
		{"40P01", database.Deadlock, 2},
		{"23505", database.UniqueViolation, 1},
	} {
		t.Run(test.state, func(t *testing.T) {
			base := int64(index * 10)
			var attempts atomic.Int64
			err := database.Retry(t.Context(), db, policy, func(tx *database.Tx) error {
				attempt := attempts.Add(1)
				if _, err := tx.Exec(t.Context(), "INSERT INTO retry_records VALUES ($1)", base+attempt); err != nil {
					return err
				}
				if attempt > 1 {
					return nil
				}
				_, err := tx.Exec(t.Context(), "DO $$ BEGIN RAISE EXCEPTION 'conflict' USING ERRCODE = '"+test.state+"'; END $$")
				return err
			})
			if attempts.Load() != test.attempts {
				t.Fatal("unexpected attempts", attempts.Load(), err)
			}
			first := countWhere(t, db, base+1)
			second := countWhere(t, db, base+2)
			if test.attempts == 1 {
				var failure *database.Error
				if !errors.Is(err, test.code) || !errors.As(err, &failure) || failure.Outcome() != database.RolledBack || first != 0 || second != 0 {
					t.Fatal("non-retryable rollback was retried or left effects", err, first, second)
				}
				return
			}
			if err != nil || first != 0 || second != 1 {
				t.Fatal("retry kept the failed attempt's effects or did not commit", err, first, second)
			}
		})
	}
}

func countWhere(t *testing.T, db *database.DB, id int64) int64 {
	t.Helper()
	var count int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM retry_records WHERE id = $1", []any{id}, &count); err != nil {
		t.Fatal(err)
	}
	return count
}
