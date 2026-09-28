package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLockedReadsRequireTransactionAndDiscardPartialHydration(t *testing.T) {
	q := mutationModel().ForUpdate()
	if _, err := q.All(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil lock transaction accepted", err)
	}
	for _, test := range []struct {
		name       string
		values     [][]driver.Value
		closeError error
	}{
		{"codec", [][]driver.Value{{int64(1), "one"}, {"bad", "two"}}, nil},
		{"close", [][]driver.Value{{int64(1), "one"}}, errors.New("close failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &driverState{}
			state.query = func(_ context.Context, sql string, _ []driver.NamedValue) (driver.Rows, error) {
				if !strings.HasSuffix(sql, " FOR UPDATE") {
					t.Error("read lost row lock", sql)
				}
				return &resultRows{state: state, columns: []string{"id", "name"}, values: test.values, closeError: test.closeError}, nil
			}
			db := open(t, state, nil)
			err := db.Transaction(t.Context(), func(tx *database.Tx) error {
				items, err := q.All(t.Context(), tx)
				if err == nil || items != nil {
					t.Fatal("failed locked read exposed partial models", err)
				}
				if state.rowsClosed.Load() != 1 || state.rolledBack.Load() != 0 {
					t.Fatal("locked read did not close rows or took over transaction")
				}
				return err
			})
			if err == nil || state.rolledBack.Load() != 1 {
				t.Fatal("caller rollback missing", err)
			}
		})
	}
}

func TestLockedFirstSharesOrderingAndCallerTransaction(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(7), "seven"}})
	read := state.query
	state.query = func(ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(sql, `ORDER BY "records"."id" ASC LIMIT $1 FOR KEY SHARE`) || len(args) != 1 || args[0].Value != int64(1) {
			t.Error("locked First lacks default ordering/limit", sql)
		}
		return read(ctx, sql, args)
	}
	db := open(t, state, nil)
	var escaped *database.Tx
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		escaped = tx
		m, err := mutationModel().ForKeyShare().RequireFirst(t.Context(), tx)
		if err != nil || m.ID != 7 {
			t.Fatal("locked hydration failed", err)
		}
		return nil
	})
	if err != nil || state.committed.Load() != 1 {
		t.Fatal("lock read owns another transaction", err)
	}
	if _, err := mutationModel().ForUpdate().All(t.Context(), escaped); !errors.Is(err, database.Closed) {
		t.Fatal("escaped transaction reused", err)
	}
	if _, err := mutationModel().ForUpdate().All(nil, escaped); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mutationModel().ForUpdate().All(ctx, escaped); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled query changed error", err)
	}
	id := query.NewOrderedField[mutationRecord, int64]("records", "id", codec.Signed[int64]())
	if _, err := query.SelectValue(mutationModel(), id.Value()).ForUpdate().All(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("scalar locked read accepted nil transaction", err)
	}
}
