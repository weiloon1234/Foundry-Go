package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// These are the selected-scalar inspection assertions retained in framequeries,
// expressionqueries, scalarqueries and keyqueries. No surrogate DTO is needed.
func compileReadParity[V any](t *testing.T, oldValue query.Expression[scalars.Record, V], newValue expr.Selected[scalars.Record, V], sample driver.Value) {
	t.Helper()
	oldQuery := query.SelectValue(scalarOldSource(), oldValue).Limit(4).Offset(1)
	newQuery := rowset.ReadScalar(rowset.SelectScalar(rowset.From(scalars.Records.Query()), newValue)).Limit(4).Offset(1)
	readTerminalParity(t, oldQuery, newQuery, sample)
}

func readTerminalParity[S, V any](t *testing.T, oldQuery interface {
	Compile() (query.Statement, error)
	Each(context.Context, database.Executor, func(V) error) error
}, newQuery rowset.ReadSource[S, V], sample driver.Value) {
	t.Helper()
	before, err := oldQuery.Compile()
	if err != nil {
		t.Fatal(err)
	}
	after, err := rowset.CompileRead(newQuery)
	if err != nil {
		t.Fatal(err)
	}
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) {
		t.Fatalf("compile parity:\n%s\n%s\n%v\n%v", before.SQL(), after.SQL(), before.Arguments(), after.Arguments())
	}
	if err := rowset.PreflightRead(t.Context(), newQuery); err != nil {
		t.Fatal(err)
	}
	for _, api := range []string{"legacy", "orm"} {
		g := &goldenCapture{scriptedColumns: []string{"value"}, scriptedBatches: [][][]driver.Value{{{sample}, {sample}}}}
		db := g.open(t.Context())
		stop := errors.New("stop computed scalar")
		seen := 0
		yield := func(V) error { seen++; return stop }
		if api == "legacy" {
			err = oldQuery.Each(t.Context(), db, yield)
		} else {
			err = rowset.EachRead(t.Context(), db, newQuery, yield)
		}
		if !errors.Is(err, stop) || seen != 1 || g.statements != 1 || db.Stats().InUse != 0 || db.Stats().Owners != 0 {
			t.Fatal(api, seen, err, db.Stats())
		}
		if !strings.Contains(g.output.String(), before.SQL()) {
			t.Fatal("stream changed compiled window", g.output.String())
		}
		db.Close(context.Background())
	}
}
func TestStage9ReadScalarCompileParity(t *testing.T) {
	o := query.NewTextField[scalars.Record, string]("scalar_records", "name", codec.String[string]())
	n := expr.Value(scalars.Records.Name)
	t.Run("conditional", func(t *testing.T) {
		compileReadParity(t, query.When(o.Eq("Ada"), query.Param(o, "yes")).Else(query.Param(o, "no")).Value(), expr.Select(expr.When(n.Eq("Ada"), expr.Param(scalars.Records.Name, "yes")).Else(expr.Param(scalars.Records.Name, "no"))), "yes")
	})
	t.Run("text", func(t *testing.T) { compileReadParity(t, query.Lower(o).Value(), expr.Lower(n).Value(), "ada") })
	latitude := query.NewOrderedField[scalars.Record, float64]("scalar_records", "latitude", codec.Float[float64]())
	t.Run("geographic", func(t *testing.T) {
		compileReadParity(t, query.SphericalDistance(latitude, latitude, query.GeographicPoint{}).Value(), expr.SphericalDistance(expr.Value(scalars.Records.Latitude), expr.Value(scalars.Records.Latitude), expr.GeographicPoint{}).Value(), float64(4.5))
	})
	ow := query.WindowFor(scalarOldSource()).PartitionBy(query.Param(o, "same").Group()).Named("partitioned").OrderBy(o.Asc()).Named("ordered").RowsBetween(query.Preceding(1), query.CurrentRow()).Named("framed")
	nw := expr.WindowFor(rowset.ModelScope(scalars.Records.Query())).PartitionBy(expr.Group(expr.Param(scalars.Records.Name, "same"))).Named("partitioned").OrderBy(n.Asc()).Named("ordered").RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
	t.Run("named_frame", func(t *testing.T) {
		oldRepeated := query.AddValue(query.Count[scalars.Record]().Over(ow), query.RowNumber(ow))
		newRepeated := expr.AddValue(expr.Over(expr.Count[scalars.Record](), nw), expr.RowNumber(nw))
		compileReadParity(t, oldRepeated, newRepeated, int64(2))
		inspected, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(scalars.Records.Query()), newRepeated)))
		if err != nil || strings.Count(inspected.SQL(), `"framed" AS (`) != 1 || len(inspected.Arguments()) != 2 {
			t.Fatal("named definitions or bindings duplicated", inspected, err)
		}
	})
	ob := query.When(o.Eq("Ada"), query.Param(o, "yes")).Else(query.Param(o, "no"))
	nb := expr.When(n.Eq("Ada"), expr.Param(scalars.Records.Name, "yes")).Else(expr.Param(scalars.Records.Name, "no"))
	t.Run("computed_key", func(t *testing.T) {
		compileReadParity(t, query.RowNumber(query.WindowFor(scalarOldSource()).PartitionByValues(ob.Value().Key()).OrderBy(o.Asc())), expr.RowNumber(expr.WindowFor(rowset.ModelScope(scalars.Records.Query())).PartitionByValues(expr.Select(nb).Key()).OrderBy(n.Asc())), int64(1))
	})
}

func TestStage9ReadCTEScalarAndValueSetParity(t *testing.T) {
	oldCTE := query.As[expressionSide](query.CTE("eligible", scalarOldSource().Limit(9)), "r")
	oldName := query.NewTextField[query.Alias[expressionSide, scalars.Record], string]("r", "name", codec.String[string]())
	newCTE := rowset.AsRecord[expressionSide](rowset.CTE("eligible", rowset.ModelRecord(scalars.Records.Query().Limit(9))), "r")
	newName := expr.At(rowset.Scope(newCTE), scalars.Records.Name)
	oldScalar := query.SelectValue(oldCTE, query.Value(oldName)).OrderBy(oldName.Asc()).Limit(3).Offset(1)
	newScalar := rowset.ReadScalar(rowset.SelectScalar(newCTE, expr.Select(newName))).OrderBy(newName.Asc()).Limit(3).Offset(1)
	t.Run("cte", func(t *testing.T) { readTerminalParity(t, oldScalar, newScalar, "Ada") })
	values := rowset.Values(rowset.SelectScalar(newCTE, expr.Select(newName)))
	newValues := rowset.UnionAllValues(values, values)
	// The set retains one shared CTE, then applies its completed output window.
	oldPlain := query.SelectValue(oldCTE, query.Value(oldName))
	oldValues := oldPlain.UnionAll(oldPlain)
	oldValues = oldValues.OrderBy(oldValues.Value().Asc()).Limit(3).Offset(1)
	newValues = newValues.OrderBy(newValues.Value().Asc()).Limit(3).Offset(1)
	t.Run("value_set", func(t *testing.T) { readTerminalParity(t, oldValues, rowset.ReadValues(newValues), "Ada") })
}
