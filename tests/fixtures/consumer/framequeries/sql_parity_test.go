package framequeries_test

import (
	"foundry.test/consumer/framequeries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"
)

func TestNamedFrameSQLParity(t *testing.T) {
	f := framequeries.Samples
	if err := orm.Check(f); err != nil {
		t.Fatal(err)
	}
	q := f.Query()
	window := expr.WindowFor(rows.ModelScope(q)).PartitionBy(expr.Group(expr.Param(f.ID, 1))).OrderBy(expr.Value(f.ID).Asc()).RowsBetween(expr.Preceding(1), expr.CurrentRow()).Named("framed")
	selected := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(q), expr.Over(expr.Count[framequeries.Sample](), window)), expr.RowNumber(window).Asc()))
	if err := rows.PreflightRead(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	got, err := rows.CompileRead(selected)
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/framequeries/sql_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
}
