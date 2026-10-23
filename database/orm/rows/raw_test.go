package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestRawDeclaredRows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns []string
		values  [][]driver.Value
		invalid bool
	}{
		{"reorder", []string{"name", "id"}, [][]driver.Value{{"Ada", int64(9)}}, false},
		{"missing_zero", []string{"id"}, nil, true},
		{"duplicate", []string{"id", "id"}, nil, true},
		{"extra", []string{"id", "name", "extra"}, nil, true},
		{"decode", []string{"id", "name"}, [][]driver.Value{{int64(9), "Ada"}, {"bad", "private"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &driverState{columns: tc.columns, rows: tc.values}
			got, err := Raw(t.Context(), openTest(t, state), declareRows(), "raw")
			if !state.closed {
				t.Fatal("rows not closed")
			}
			if tc.invalid {
				if err == nil || got != nil {
					t.Fatal(got, err)
				}
			} else if err != nil || len(got) != 1 || got[0] != (record{9, "Ada"}) {
				t.Fatal(got, err)
			}
		})
	}
}
func TestRawRowDeclarationAndCancellationBeforeSQL(t *testing.T) {
	state := &driverState{}
	db := openTest(t, state)
	var schema Row[record]
	if got, err := Raw(t.Context(), db, schema, "must not run"); err == nil || got != nil || state.sql != "" {
		t.Fatal(got, err, state.sql)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := Raw(ctx, db, declareRows(), "must not run"); !errors.Is(err, context.Canceled) || got != nil || state.sql != "" {
		t.Fatal(got, err, state.sql)
	}
}
