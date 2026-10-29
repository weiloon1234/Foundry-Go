package scalarqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"github.com/weiloon1234/Foundry-Go/value"

	"testing"
)

func TestCalculationProjectionSQLParity(t *testing.T) {
	f, r := scalarqueries.Samples, scalarqueries.Calculations
	selected := rows.Select(rows.From(f.Query()), r.Row,
		rows.Map(r.ID, expr.Value(orm.NumberKeyExpr(f.ID))),
		rows.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(orm.TextExpr(f.Name)))).Value()),
		rows.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(orm.NumberExpr(f.Quantity))), expr.Value(orm.NumberExpr(f.Amount))).Value()),
		rows.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax))).Value()))
	got, err := rows.CompileRead(rows.Read(selected))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/scalarqueries/projection_sql_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
}

func TestDerivedAggregateSQLParity(t *testing.T) {
	f, r := scalarqueries.Samples, scalarqueries.Calculations
	selected := rows.Select(rows.From(f.Query().Where(orm.NumberGt(f.Quantity, 0))), r.Row,
		rows.Map(r.ID, expr.Value(orm.NumberKeyExpr(f.ID))), rows.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(orm.TextExpr(f.Name)))).Value()),
		rows.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(orm.NumberExpr(f.Quantity))), expr.Value(orm.NumberExpr(f.Amount))).Value()),
		rows.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax))).Value()))
	derived := rows.AsRecord[computedLines](rows.CTE("computed_lines", rows.RecordOf(selected)), "lines")
	line, tax := rows.At(rows.Scope(derived), r.LineTotal), rows.At(rows.Scope(derived), r.AmountWithTax)

	for _, tc := range []struct {
		name    string
		current expr.Selected[rows.Alias[computedLines, scalarqueries.Calculation], value.Nullable[decimal.Decimal]]
	}{
		{"sum", expr.SumRow(line).Value()},
		{"avg", expr.AvgRow(line).Value()},
		{"nullable_sum", expr.SumNullableRow(tax).Value()},
		{"nullable_avg", expr.AvgNullableRow(tax).Value()},
		{"min", expr.MinRow(line).Value()},
		{"max", expr.MaxRow(line).Value()},
		{"nullable_min", expr.MinNullableRow(tax).Value()},
		{"nullable_max", expr.MaxNullableRow(tax).Value()},
		{"filtered", expr.SumRow(line).Filter(expr.OrderRow(line).Gt(decimal.FromInt64(10))).Value()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(derived, tc.current)))
			if err != nil {
				t.Fatal(err)
			}

			if !oracleEqual(t, "consumer/scalarqueries/projection_sql_test/2", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
				t.Fatal("recorded SQL parity changed")
			}
		})
	}
}

func TestDerivedCountSQLParity(t *testing.T) {
	f, r := scalarqueries.Samples, scalarqueries.Calculations
	selected := rows.Select(rows.From(f.Query()), r.Row,
		rows.Map(r.ID, expr.Value(orm.NumberKeyExpr(f.ID))), rows.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(orm.TextExpr(f.Name)))).Value()),
		rows.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(orm.NumberExpr(f.Quantity))), expr.Value(orm.NumberExpr(f.Amount))).Value()),
		rows.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax))).Value()))
	derived := rows.AsRecord[computedLines](rows.CTE("computed_lines", rows.RecordOf(selected)), "lines")
	tax := rows.At(rows.Scope(derived), r.AmountWithTax)

	for _, tc := range []struct {
		name    string
		current expr.Selected[rows.Alias[computedLines, scalarqueries.Calculation], int64]
	}{
		{"count", expr.CountRow(tax).Value()},
		{"distinct", expr.CountDistinctRow(tax).Value()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(derived, tc.current)))
			if err != nil {
				t.Fatal(err)
			}

			if !oracleEqual(t, "consumer/scalarqueries/projection_sql_test/3", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
				t.Fatal("recorded SQL parity changed")
			}
		})
	}
}
