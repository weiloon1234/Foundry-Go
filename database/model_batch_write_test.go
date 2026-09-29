package database_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// PostgreSQL caches 64 subtransaction IDs per backend; a savepoint per batch
// row overflows it. Rows run directly in the atomic batch transaction instead.
func TestPerModelBatchWritesRunWithoutPerRowSavepoints(t *testing.T) {
	var savepoints, next atomic.Int64
	state := &driverState{}
	state.exec = func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
		if strings.HasPrefix(statement, "SAVEPOINT ") {
			savepoints.Add(1)
		}
		return driver.RowsAffected(0), nil
	}
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{next.Add(1), "stored"}}}, nil
	}
	db := open(t, state, nil)
	mutations := make([]query.Mutation[mutationRecord], 70)
	for i := range mutations {
		mutations[i] = mutationValues()
	}
	created, err := mutationModel().InsertEach(t.Context(), db, mutations)
	if err != nil || len(created) != len(mutations) || created[69].ID != 70 || savepoints.Load() != 0 || state.committed.Load() != 1 {
		t.Fatal("pool batch used per-row savepoints or lost rows", len(created), savepoints.Load(), err)
	}
	// A caller-owned transaction keeps one savepoint around the whole batch so
	// a failed batch can still be recovered from without per-row subtransactions.
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := mutationModel().InsertEach(t.Context(), tx, mutations)
		return err
	})
	if err != nil || savepoints.Load() != 1 || state.committed.Load() != 2 {
		t.Fatal("transaction batch did not use exactly one enclosing savepoint", savepoints.Load(), err)
	}
}
