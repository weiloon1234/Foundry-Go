package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
			c := rows.CursorFor(rows.ModelRecord(mutationModel()))
			id := expr.At(c.Scope(), orm.KeyExpr(mutationFields.Key()))
			page, err := c.UniqueBy(expr.Group(id)).Paginate(t.Context(), db, orm.CursorRequest[mutationRecord]{Size: 1})
			if kind == "success" {
				if err != nil || !reflect.DeepEqual(page.Items, []mutationRecord{{ID: 1, Name: "first"}}) || !page.Next.IsSet() || page.Previous.IsSet() {
					t.Fatal("wrong cursor page", page, err)
				}
			} else if err == nil || !reflect.DeepEqual(page, orm.CursorPage[mutationRecord]{}) {
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
