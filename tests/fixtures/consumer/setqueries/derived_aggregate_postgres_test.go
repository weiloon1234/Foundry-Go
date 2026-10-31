package setqueries_test

import (
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"testing"
)

func checkDerivedSetAggregation(t *testing.T, tx *database.Tx) error {
	reportsQuery := derivedReports()
	r := reports.BuyerTotalRows
	total := rows.At(reportsQuery.Scope(), r.Total)
	results, err := rows.ReadSet(reportsQuery.Where(expr.NullableGte(expr.OrderNullableRow(total), decimal.FromInt64(20))).OrderBy(expr.Asc(total))).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(results) != 3 {
		t.Fatal("set of different input scopes lost output filtering")
	}
	if amount, ok := results[0].Total.Get(); !ok || amount.String() != "20" {
		t.Error("set changed exact decimal codec")
	}
	// Project and group the combined result through the same projection runtime.
	s := reports.OrderSummaries
	summary := rows.Select(reportsQuery, s.Row,
		rows.MapSelected(s.Total, expr.AsSelected(expr.SumNullableRow(total))),
		rows.MapSelected(s.Count, expr.AsSelected(expr.Count[rows.Set[reports.BuyerTotals]]())))
	row, err := rows.Read(summary).RequireFirst(t.Context(), tx)
	if err != nil {
		return err
	}
	if row.Count != 5 {
		t.Error("projection over set lost rows")
	}
	if total, ok := row.Total.Get(); !ok || total.String() != "69" {
		t.Error("projection over set lost exact aggregate")
	}
	return nil
}
