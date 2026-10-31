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
		rows.MapSelected(r.Caption, expr.AsSelected(expr.Lower(expr.Trim(expr.Value(orm.TextExpr(f.Name)))))),
		rows.MapSelected(r.LineTotal, expr.AsSelected(expr.Multiply(expr.DecimalOf(expr.Value(orm.NumberExpr(f.Quantity))), expr.Value(orm.NumberExpr(f.Amount))))),
		rows.MapSelected(r.AmountWithTax, expr.AsSelected(expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax))))))
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
		rows.Map(r.ID, expr.Value(orm.NumberKeyExpr(f.ID))), rows.MapSelected(r.Caption, expr.AsSelected(expr.Lower(expr.Trim(expr.Value(orm.TextExpr(f.Name)))))),
		rows.MapSelected(r.LineTotal, expr.AsSelected(expr.Multiply(expr.DecimalOf(expr.Value(orm.NumberExpr(f.Quantity))), expr.Value(orm.NumberExpr(f.Amount))))),
		rows.MapSelected(r.AmountWithTax, expr.AsSelected(expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax))))))
	derived := rows.AsRecord[computedLines](rows.CTE("computed_lines", rows.RecordOf(selected)), "lines")
	line, tax := rows.At(rows.Scope(derived), r.LineTotal), rows.At(rows.Scope(derived), r.AmountWithTax)

	for _, tc := range []struct {
		name    string
		current expr.Selected[rows.Alias[computedLines, scalarqueries.Calculation], value.Nullable[decimal.Decimal]]
	}{
		{"sum", expr.AsSelected(expr.SumRow(line))},
		{"avg", expr.AsSelected(expr.AvgRow(line))},
		{"nullable_sum", expr.AsSelected(expr.SumNullableRow(tax))},
		{"nullable_avg", expr.AsSelected(expr.AvgNullableRow(tax))},
		{"min", expr.AsSelected(expr.MinRow(line))},
		{"max", expr.AsSelected(expr.MaxRow(line))},
		{"nullable_min", expr.AsSelected(expr.MinNullableRow(tax))},
		{"nullable_max", expr.AsSelected(expr.MaxNullableRow(tax))},
		{"filtered", expr.AsSelected(expr.Filter(expr.SumRow(line), expr.Gt(expr.OrderRow(line), decimal.FromInt64(10))))},
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
		rows.Map(r.ID, expr.Value(orm.NumberKeyExpr(f.ID))), rows.MapSelected(r.Caption, expr.AsSelected(expr.Lower(expr.Trim(expr.Value(orm.TextExpr(f.Name)))))),
		rows.MapSelected(r.LineTotal, expr.AsSelected(expr.Multiply(expr.DecimalOf(expr.Value(orm.NumberExpr(f.Quantity))), expr.Value(orm.NumberExpr(f.Amount))))),
		rows.MapSelected(r.AmountWithTax, expr.AsSelected(expr.AddNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Amount))), expr.Value(orm.NullableNumberExpr(f.Tax))))))
	derived := rows.AsRecord[computedLines](rows.CTE("computed_lines", rows.RecordOf(selected)), "lines")
	tax := rows.At(rows.Scope(derived), r.AmountWithTax)

	for _, tc := range []struct {
		name    string
		current expr.Selected[rows.Alias[computedLines, scalarqueries.Calculation], int64]
	}{
		{"count", expr.AsSelected(expr.CountRow(tax))},
		{"distinct", expr.AsSelected(expr.CountDistinctRow(tax))},
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
