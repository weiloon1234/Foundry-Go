package database_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

func TestQueryCancellationDuringDriverReturnReleasesRows(t *testing.T) {
	state := &driverState{}
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen, c.MaxIdle = 1, 1 })
	// Cancel at the boundary where a successful driver query returns its rows,
	// before Foundry registers its cancellation hook. Repetition exercises the
	// callback's immediate execution against initialization under the race gate.
	for iteration := range 200 {
		ctx, cancel := context.WithCancel(t.Context())
		state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			cancel()
			return &resultRows{state: state, values: [][]driver.Value{{int64(42)}}}, nil
		}
		rows, err := db.Query(ctx, "cancel while returning rows")
		cancel()
		if err != nil {
			t.Fatal("driver returned a successful row stream", err)
		}
		waitFor(t, func() bool { return db.Stats().Owners == 0 && db.Stats().InUse == 0 }, "query-return cancellation retained its owner")
		_ = rows.Close()
		if rows.Next() || state.rowsClosed.Load() != int64(iteration+1) {
			t.Fatal("canceled stream remained readable or closed its driver more than once")
		}
	}
	if _, err := db.Exec(t.Context(), "reuse after canceled queries"); err != nil {
		t.Fatal("query-return cancellation made the connection unusable", err)
	}
}
