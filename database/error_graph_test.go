package database_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

type cyclicTransactionError struct{ visits int }

func (*cyclicTransactionError) Error() string { panic("private cause must not be formatted") }
func (e *cyclicTransactionError) Unwrap() error {
	e.visits++
	// The old traversal terminates only to report a bounded regression failure.
	if e.visits > 512 {
		return nil
	}
	return e
}

func TestCyclicCallbackErrorCompletesRollbackAndReleasesPool(t *testing.T) {
	for _, nested := range []bool{false, true} {
		name := "transaction"
		if nested {
			name = "savepoint"
		}
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			var classified atomic.Int32
			db, err := database.Open(t.Context(), database.Adapter{
				Connector: connector{state},
				Classify: func(error) database.Detail {
					classified.Add(1)
					return database.Detail{Code: database.Unavailable}
				},
			}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			cause := &cyclicTransactionError{}
			err = db.Transaction(t.Context(), func(tx *database.Tx) error {
				if nested {
					return tx.Savepoint(t.Context(), func(*database.Tx) error { return cause })
				}
				return cause
			})
			var detail *database.Error
			if !errors.As(err, &detail) || detail.Code() != database.QueryFailed || detail.Outcome() != database.RolledBack {
				t.Fatal("cyclic callback lost conservative classification or rollback outcome")
			}
			if cause.visits > 256 || classified.Load() != 0 {
				t.Fatal("unbounded error reached adapter classification", cause.visits, classified.Load())
			}
			if state.rolledBack.Load() != 1 || state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("cyclic error retained database resources")
			}
			if _, err := db.Exec(t.Context(), "pool remains usable"); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
