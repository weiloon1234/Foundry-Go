package projectionqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func TestFilteredAggregatePhaseSQLParity(t *testing.T) {
	predicate := expr.ModelPredicate(models.Orders.TotalCents.Gt(0))
	total := expr.Sum(models.Orders.TotalCents).Filter(predicate)
	count := expr.Count[models.Order]().Filter(predicate)
	exists := expr.Exists[models.Order]().Filter(predicate)
	selected := rows.Select(rows.From(models.Orders.Query()), reports.OrderSummaries,
		rows.MapSelected(reports.OrderSummaries.Total, total.Value()),
		rows.MapSelected(reports.OrderSummaries.Count, count.Value()))
	got, err := rows.Compile(rows.Having(selected, expr.HavingAnd(count.Gt(0), total.Gt(decimal.FromInt64(1)), exists.Eq(true))))
	if err != nil {
		t.Fatal(err)
	}

	f := models.OrderFields()
	oldTotal := query.Sum(f.TotalCents).Filter(f.TotalCents.Gt(0))
	oldCount := query.Count[models.Order]().Filter(f.TotalCents.Gt(0))
	oldExists := query.Exists[models.Order]().Filter(f.TotalCents.Gt(0))
	oldSelected := reports.SelectOrderSummary(models.QueryOrders(), reports.OrderSummarySelection[models.Order]{Total: oldTotal.Value(), Count: oldCount.Value()})
	want, err := oldSelected.Having(query.HavingAnd(oldCount.Gt(0), oldTotal.Gt(decimal.FromInt64(1)), oldExists.Eq(true))).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/projectionqueries/aggregate_contracts_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatalf("filtered aggregate SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}

	// Row predicates/orders use the same adapters as the phase-rejection negatives.
	row := expr.OrderBy(expr.Where(models.Orders.Query(), predicate), expr.Value(models.Orders.TotalCents).Desc())
	if _, err := orm.Compile(row); err != nil {
		t.Fatalf("ordinary row phase rejected: %v", err)
	}
}
