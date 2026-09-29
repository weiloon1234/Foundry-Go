package database_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func retryDB(t *testing.T, state *driverState, code database.Code, rejected bool) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail {
		return database.Detail{Code: code, CommitRejected: rejected}
	}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	return db
}

func TestRetryRerunsOnlyConfirmedSerializationRollbacks(t *testing.T) {
	policy := database.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
	for _, test := range []struct {
		name     string
		code     database.Code
		rejected bool
		attempts int64
	}{
		{"serialization", database.SerializationFailure, true, 3},
		{"deadlock", database.Deadlock, true, 3},
		{"unknown commit", database.SerializationFailure, false, 1},
		{"unique violation", database.UniqueViolation, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &driverState{commitError: errors.New("commit failed")}
			db := retryDB(t, state, test.code, test.rejected)
			var calls, callbacks atomic.Int64
			err := database.Retry(t.Context(), db, policy, func(tx *database.Tx) error {
				calls.Add(1)
				return tx.AfterCommit(func(context.Context) error { callbacks.Add(1); return nil })
			})
			if err == nil || calls.Load() != test.attempts || callbacks.Load() != 0 {
				t.Fatal("retry ran an unexpected number of attempts", calls.Load(), err)
			}
		})
	}
	state := &driverState{}
	db := retryDB(t, state, database.SerializationFailure, true)
	var calls atomic.Int64
	if err := database.Retry(t.Context(), db, policy, func(*database.Tx) error { calls.Add(1); return nil }); err != nil || calls.Load() != 1 {
		t.Fatal("successful transaction was retried", err)
	}
	application := errors.New("application rejected input")
	if err := database.Retry(t.Context(), db, policy, func(*database.Tx) error { return application }); err != application {
		t.Fatal("application failure lost identity or was retried", err)
	}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		return database.Retry(t.Context(), tx, policy, func(*database.Tx) error { return nil })
	})
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("savepoint retry accepted", err)
	}
	for _, invalid := range []database.RetryPolicy{{}, {Attempts: database.MaxRetryAttempts + 1, InitialDelay: 1, MaxDelay: 1}, {Attempts: 1, InitialDelay: 2, MaxDelay: 1}} {
		if err := database.Retry(t.Context(), db, invalid, func(*database.Tx) error { return nil }); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid retry policy accepted", invalid)
		}
	}
}
