package scalarqueries_test

import (
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"testing"
)

func TestPostgresScalarProjectionAndDerivedAggregation(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		f, r := scalarqueries.Samples, scalarqueries.Calculations
		selected := rowset.Select(rowset.From(f.Query()), r.Row,
			rowset.Map(r.ID, expr.Value(f.ID)),
			rowset.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(f.Name))).Value()),
			rowset.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(f.Quantity)), expr.Value(f.Amount)).Value()),
			rowset.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(f.Amount)), expr.Value(f.Tax)).Value()))
		rows, err := selected.OrderBy(expr.Value(f.ID).Asc()).All(t.Context(), tx)
		if err != nil || len(rows) != 3 {
			t.Fatal(rows, err)
		}
		if rows[0].Caption != "élan_20%" {
			t.Fatal("text projection", rows[0])
		}
		if _, present := rows[1].AmountWithTax.Get(); present {
			t.Fatal("nullable projection lost NULL")
		}
		derived := rowset.AsRecord[computedLines](rowset.CTE("computed_lines", rowset.RecordOf(selected)), "lines")
		line := rowset.At(rowset.Scope(derived), r.LineTotal)
		want, _ := decimal.Parse("18014398509481997.45")
		total, err := rowset.ReadScalar(rowset.SelectScalar(derived, expr.SumRow(line).Value())).RequireFirst(t.Context(), tx)
		got, present := total.Get()
		if err != nil || !present || got != want {
			t.Fatal("exact derived arithmetic aggregation", got, err)
		}
		return nil
	})
}
