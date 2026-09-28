package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestResultCursorOwnsRowsAndDiscardsFailure(t *testing.T) {
	for _, kind := range []string{"success", "query", "lookahead", "close"} {
		t.Run(kind, func(t *testing.T) {
			state := &driverState{}
			cause := errors.New("cursor driver failure")
			calls := 0
			state.query = func(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
				calls++
				if strings.Contains(sql, "COUNT(") || len(args) != 1 || args[0].Value != int64(2) {
					t.Fatal("cursor did not use one lookahead query", sql, args)
				}
				if kind == "query" {
					return nil, cause
				}
				rows := &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "first"}, {int64(2), "second"}}}
				if kind == "lookahead" {
					rows.values[1][0] = "invalid ID"
				}
				if kind == "close" {
					rows.closeError = cause
				}
				return rows, nil
			}
			db := open(t, state, nil)
			c := query.CursorFor(mutationModel())
			id := query.NewOrderedField[query.CursorScope[mutationRecord], int64](c.Scope().Table(), "id", codec.Signed[int64]())
			page, err := c.UniqueBy(id.Group()).Paginate(t.Context(), db, query.CursorRequest[mutationRecord]{Size: 1})
			if kind == "success" {
				if err != nil || !reflect.DeepEqual(page.Items, []mutationRecord{{ID: 1, Name: "first"}}) || !page.Next.IsSet() || page.Previous.IsSet() {
					t.Fatal("wrong cursor page", page, err)
				}
			} else if err == nil || !reflect.DeepEqual(page, query.CursorPage[mutationRecord]{}) {
				t.Fatal("failure leaked page", page, err)
			}
			if (kind == "query" || kind == "close") && !errors.Is(err, cause) {
				t.Fatal("failure cause lost", err)
			}
			if calls != 1 || db.Stats().Owners != 0 || (kind != "query" && state.rowsClosed.Load() != 1) {
				t.Fatal("cursor retained row ownership or repeated query")
			}
		})
	}
}
