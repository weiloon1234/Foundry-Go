package setqueries_test

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
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
	u, o := models.UserFields(), models.OrderFields()
	orders := reports.ProjectBuyerTotals(models.QueryOrders()).SelectBuyerID(query.Value(o.BuyerID)).SelectTotal(query.Sum(o.TotalCents).Value()).SelectOrders(query.Count[models.Order]().Value()).SelectAverage(query.Avg(o.TotalCents).Value()).Query().GroupBy(o.BuyerID.Group())
	people := reports.ProjectBuyerTotals(models.QueryUsers()).SelectBuyerID(query.Value(u.ID)).SelectTotal(query.Sum(u.Age).Value()).SelectOrders(query.Count[models.User]().Value()).SelectAverage(query.Avg(u.Age).Value()).Query().GroupBy(u.ID.Group())
	legacy := orders.UnionAll(people)
	fields := reports.BuyerTotalsFieldsAt(legacy.Scope())
	want, err := reports.ProjectOrderSummary(legacy).SelectTotal(query.Sum(fields.Total).Value()).SelectCount(query.Count[query.Set[reports.BuyerTotals]]().Value()).Query().Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/setqueries/derived_aggregate_sql_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("derived set aggregation parity\n%s\n%s", got.SQL(), want.SQL())
	}
}
