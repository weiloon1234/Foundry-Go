package setqueries_test

import (
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"
)

func TestDerivedSetAggregateSQLParity(t *testing.T) {
	source := derivedReports()
	total := rows.At(source.Scope(), reports.BuyerTotalRows.Total)
	r := reports.OrderSummaries
	selected := rows.Select(source, r.Row, rows.MapSelected(r.Total, expr.SumNullableRow(total).Value()), rows.MapSelected(r.Count, expr.Count[rows.Set[reports.BuyerTotals]]().Value()))
	got, err := rows.CompileRead(rows.Read(selected))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/setqueries/derived_aggregate_sql_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
}
