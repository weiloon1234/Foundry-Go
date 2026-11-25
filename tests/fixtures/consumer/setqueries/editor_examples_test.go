package setqueries_test

import (
	"testing"

	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestCompileCombinedReportExample(t *testing.T) {
	source := derivedReports()
	total := rows.At(source.Scope(), reports.BuyerTotalRows.Total)
	r := reports.OrderSummaries
	selected := rows.Select(source, r.Row,
		rows.MapSelected(r.Total, expr.AsSelected(expr.SumNullableRow(total))),
		rows.MapSelected(r.Count, expr.AsSelected(expr.Count[rows.Set[reports.BuyerTotals]]())))
	if _, err := rows.CompileRead(rows.Read(selected)); err != nil {
		t.Fatal(err)
	}
}

var derivedReports = reports.DerivedReports
