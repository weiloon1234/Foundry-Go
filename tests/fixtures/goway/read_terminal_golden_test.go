package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"foundry.test/goway/models/scalars"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// These are the selected-scalar inspection assertions retained in framequeries,
// expressionqueries, scalarqueries and keyqueries. No surrogate DTO is needed.
func compileReadParity[V any](t *testing.T, newValue expr.Selected[scalars.Record, V], sample driver.Value) {
	t.Helper()

	newQuery := rowset.ReadScalar(rowset.SelectScalar(rowset.From(scalars.Records.Query()), newValue)).Limit(4).Offset(1)
	readTerminalParity(t, newQuery, sample)
}

func readTerminalParity[S, V any](t *testing.T,

	newQuery rowset.ReadSource[S, V], sample driver.Value) {
	t.Helper()

	after, err := rowset.CompileRead(newQuery)
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "read_terminal/compile", oracleSnapshot([]any{after.SQL(), after.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
	if err := rowset.PreflightRead(t.Context(), newQuery); err != nil {
		t.Fatal(err)
	}
	var transcripts []string
	for _, api := range []string{"orm"} {
		g := &goldenCapture{scriptedColumns: []string{"value"}, scriptedBatches: [][][]driver.Value{{{sample}, {sample}}}}
		db := g.open(t.Context())
		stop := errors.New("stop computed scalar")
		seen := 0
		yield := func(v V) error { seen++; g.show(v); return stop }
		{
			err = rowset.EachRead(t.Context(), db, newQuery, yield)
		}
		if !errors.Is(err, stop) || seen != 1 || g.statements != 1 || db.Stats().InUse != 0 || db.Stats().Owners != 0 {
			t.Fatal(api, seen, err, db.Stats())
		}

		db.Close(context.Background())
		transcripts = append(transcripts, g.output.String())
	}
	if !oracleEqual(t, "read_terminal/stream", transcripts[0]) {
		t.Fatal("stream parity")
	}
}
func TestStage9ReadScalarCompileParity(t *testing.T) {
	n := expr.Value(scalars.Records.Name)
	t.Run("conditional", func(t *testing.T) {
		compileReadParity(t, expr.Select(expr.When(n.Eq("Ada"), expr.Param(scalars.Records.Name, "yes")).Else(expr.Param(scalars.Records.Name, "no"))), "yes")
	})
	t.Run("text", func(t *testing.T) { compileReadParity(t, expr.Lower(n).Value(), "ada") })

	t.Run("geographic", func(t *testing.T) {
		compileReadParity(t, expr.SphericalDistance(expr.Value(scalars.Records.Latitude), expr.Value(scalars.Records.Latitude), expr.GeographicPoint{}).Value(), float64(4.5))
	})

	nw := expr.WindowFor(rowset.ModelScope(scalars.Records.Query())).PartitionBy(expr.Group(expr.Param(scalars.Records.Name, "same"))).Named("partitioned").OrderBy(n.Asc()).Named("ordered").RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
	t.Run("named_frame", func(t *testing.T) {
		newRepeated := expr.AddValue(expr.Over(expr.Count[scalars.Record](), nw), expr.RowNumber(nw))
		compileReadParity(t, newRepeated, int64(2))
		inspected, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(scalars.Records.Query()), newRepeated)))
		if err != nil || strings.Count(inspected.SQL(), `"framed" AS (`) != 1 || len(inspected.Arguments()) != 2 {
			t.Fatal("named definitions or bindings duplicated", inspected, err)
		}
	})

	nb := expr.When(n.Eq("Ada"), expr.Param(scalars.Records.Name, "yes")).Else(expr.Param(scalars.Records.Name, "no"))
	t.Run("computed_key", func(t *testing.T) {
		compileReadParity(t, expr.RowNumber(expr.WindowFor(rowset.ModelScope(scalars.Records.Query())).PartitionByValues(expr.Select(nb).Key()).OrderBy(n.Asc())), int64(1))
	})
}

func TestStage9ReadCTEScalarAndValueSetParity(t *testing.T) {
	newCTE := rowset.AsRecord[expressionSide](rowset.CTE("eligible", rowset.ModelRecord(scalars.Records.Query().Limit(9))), "r")
	newName := expr.At(rowset.Scope(newCTE), scalars.Records.Name)

	newScalar := rowset.ReadScalar(rowset.SelectScalar(newCTE, expr.Select(newName))).OrderBy(newName.Asc()).Limit(3).Offset(1)
	t.Run("cte", func(t *testing.T) { readTerminalParity(t, newScalar, "Ada") })
	values := rowset.Values(rowset.SelectScalar(newCTE, expr.Select(newName)))
	newValues := rowset.UnionAllValues(values, values)
	// The set retains one shared CTE, then applies its completed output window.

	newValues = newValues.OrderBy(newValues.Value().Asc()).Limit(3).Offset(1)
	t.Run("value_set", func(t *testing.T) { readTerminalParity(t, rowset.ReadValues(newValues), "Ada") })
}
