package scalarqueries_test

import (
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"testing"
)

func TestCalculationProjectionSQLParity(t *testing.T) {
	f, r := scalarqueries.Samples, scalarqueries.Calculations
	selected := rows.Select(rows.From(f.Query()), r.Row,
		rows.Map(r.ID, expr.Value(f.ID)),
		rows.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(f.Name))).Value()),
		rows.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(f.Quantity)), expr.Value(f.Amount)).Value()),
		rows.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(f.Amount)), expr.Value(f.Tax)).Value()))
	got, err := rows.CompileRead(rows.Read(selected))
	if err != nil {
		t.Fatal(err)
	}
	old := scalarqueries.SampleFields()
	want, err := scalarqueries.SelectCalculation(scalarqueries.QueryCalculationSamples(), scalarqueries.CalculationSelection[scalarqueries.Sample]{ID: query.Value(old.ID), Caption: query.Lower(query.Trim(old.Name)).Value(), LineTotal: query.Multiply(query.DecimalOf(old.Quantity), old.Amount).Value(), AmountWithTax: query.AddNullable(query.NullableRow(old.Amount), old.Tax).Value()}).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/scalarqueries/projection_sql_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("calculation projection parity\n%s\n%s", got.SQL(), want.SQL())
	}
}

func TestDerivedAggregateSQLParity(t *testing.T) {
	f, r := scalarqueries.Samples, scalarqueries.Calculations
	selected := rows.Select(rows.From(f.Query().Where(f.Quantity.Gt(0))), r.Row,
		rows.Map(r.ID, expr.Value(f.ID)), rows.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(f.Name))).Value()),
		rows.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(f.Quantity)), expr.Value(f.Amount)).Value()),
		rows.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(f.Amount)), expr.Value(f.Tax)).Value()))
	derived := rows.AsRecord[computedLines](rows.CTE("computed_lines", rows.RecordOf(selected)), "lines")
	line, tax := rows.At(rows.Scope(derived), r.LineTotal), rows.At(rows.Scope(derived), r.AmountWithTax)
	old := scalarqueries.SampleFields()
	legacy := scalarqueries.SelectCalculation(scalarqueries.QueryCalculationSamples().Where(old.Quantity.Gt(0)), scalarqueries.CalculationSelection[scalarqueries.Sample]{ID: query.Value(old.ID), Caption: query.Lower(query.Trim(old.Name)).Value(), LineTotal: query.Multiply(query.DecimalOf(old.Quantity), old.Amount).Value(), AmountWithTax: query.AddNullable(query.NullableRow(old.Amount), old.Tax).Value()})
	oldDerived := query.As[computedLines](query.CTE("computed_lines", legacy), "lines")
	oldLine := scalarqueries.CalculationFieldsAt(oldDerived.Scope())
	for _, tc := range []struct {
		name    string
		current expr.Selected[rows.Alias[computedLines, scalarqueries.Calculation], value.Nullable[decimal.Decimal]]
		legacy  query.Expression[query.Alias[computedLines, scalarqueries.Calculation], value.Nullable[decimal.Decimal]]
	}{
		{"sum", expr.SumRow(line).Value(), query.Sum(oldLine.LineTotal).Value()},
		{"avg", expr.AvgRow(line).Value(), query.Avg(oldLine.LineTotal).Value()},
		{"nullable_sum", expr.SumNullableRow(tax).Value(), query.Sum(oldLine.AmountWithTax).Value()},
		{"nullable_avg", expr.AvgNullableRow(tax).Value(), query.Avg(oldLine.AmountWithTax).Value()},
		{"min", expr.MinRow(line).Value(), query.Min(oldLine.LineTotal).Value()},
		{"max", expr.MaxRow(line).Value(), query.Max(oldLine.LineTotal).Value()},
		{"nullable_min", expr.MinNullableRow(tax).Value(), query.Min(oldLine.AmountWithTax).Value()},
		{"nullable_max", expr.MaxNullableRow(tax).Value(), query.Max(oldLine.AmountWithTax).Value()},
		{"filtered", expr.SumRow(line).Filter(expr.OrderRow(line).Gt(decimal.FromInt64(10))).Value(), query.Sum(oldLine.LineTotal).Filter(oldLine.LineTotal.Gt(decimal.FromInt64(10))).Value()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(derived, tc.current)))
			if err != nil {
				t.Fatal(err)
			}
			want, err := query.SelectValue(oldDerived, tc.legacy).Compile()
			if err != nil {
				t.Fatal(err)
			}
			if !oracleEqual(t, "consumer/scalarqueries/projection_sql_test/2", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
				t.Fatal("recorded SQL parity changed")
			}
			if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
				t.Fatalf("derived aggregate parity\n%s\n%s", got.SQL(), want.SQL())
			}
		})
	}
}

func TestDerivedCountSQLParity(t *testing.T) {
	f, r := scalarqueries.Samples, scalarqueries.Calculations
	selected := rows.Select(rows.From(f.Query()), r.Row,
		rows.Map(r.ID, expr.Value(f.ID)), rows.MapSelected(r.Caption, expr.Lower(expr.Trim(expr.Value(f.Name))).Value()),
		rows.MapSelected(r.LineTotal, expr.Multiply(expr.DecimalOf(expr.Value(f.Quantity)), expr.Value(f.Amount)).Value()),
		rows.MapSelected(r.AmountWithTax, expr.AddNullable(expr.NullableRow(expr.Value(f.Amount)), expr.Value(f.Tax)).Value()))
	derived := rows.AsRecord[computedLines](rows.CTE("computed_lines", rows.RecordOf(selected)), "lines")
	tax := rows.At(rows.Scope(derived), r.AmountWithTax)
	old := scalarqueries.SampleFields()
	legacy := scalarqueries.SelectCalculation(scalarqueries.QueryCalculationSamples(), scalarqueries.CalculationSelection[scalarqueries.Sample]{ID: query.Value(old.ID), Caption: query.Lower(query.Trim(old.Name)).Value(), LineTotal: query.Multiply(query.DecimalOf(old.Quantity), old.Amount).Value(), AmountWithTax: query.AddNullable(query.NullableRow(old.Amount), old.Tax).Value()})
	oldDerived := query.As[computedLines](query.CTE("computed_lines", legacy), "lines")
	oldLine := scalarqueries.CalculationFieldsAt(oldDerived.Scope())
	for _, tc := range []struct {
		name    string
		current expr.Selected[rows.Alias[computedLines, scalarqueries.Calculation], int64]
		legacy  query.Expression[query.Alias[computedLines, scalarqueries.Calculation], int64]
	}{
		{"count", expr.CountRow(tax).Value(), query.CountField(oldLine.AmountWithTax).Value()},
		{"distinct", expr.CountDistinctRow(tax).Value(), query.CountDistinctField(oldLine.AmountWithTax).Value()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(derived, tc.current)))
			if err != nil {
				t.Fatal(err)
			}
			want, err := query.SelectValue(oldDerived, tc.legacy).Compile()
			if err != nil {
				t.Fatal(err)
			}
			if !oracleEqual(t, "consumer/scalarqueries/projection_sql_test/3", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
				t.Fatal("recorded SQL parity changed")
			}
			if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
				t.Fatalf("derived COUNT parity\n%s\n%s", got.SQL(), want.SQL())
			}
		})
	}
}
