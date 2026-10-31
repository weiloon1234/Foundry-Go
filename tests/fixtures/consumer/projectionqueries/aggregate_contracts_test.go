package projectionqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

func TestFilteredAggregatePhaseSQLParity(t *testing.T) {
	predicate := expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 0))
	total := expr.Filter(expr.Sum(orm.NumberExpr(models.Orders.TotalCents)), predicate)
	count := expr.Filter(expr.Count[models.Order](), predicate)
	exists := expr.Filter(expr.Exists[models.Order](), predicate)
	selected := rows.Select(rows.From(models.Orders.Query()), reports.OrderSummaries,
		rows.MapSelected(reports.OrderSummaries.Total, expr.AsSelected(total)),
		rows.MapSelected(reports.OrderSummaries.Count, expr.AsSelected(count)))
	got, err := rows.CompileRead(rows.Read(rows.Having(selected, expr.HavingAnd(expr.GtValue(count, 0), expr.GtValue(total, decimal.FromInt64(1)), expr.EqValue(exists, true)))))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/projectionqueries/aggregate_contracts_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal(

			// Row predicates/orders use the same adapters as the phase-rejection negatives.
			"frozen legacy comparison differs")
	}

	row := expr.OrderBy(expr.Where(models.Orders.Query(), predicate), expr.Desc(expr.Value(orm.NumberExpr(models.Orders.TotalCents))))
	if _, err := orm.Compile(row); err != nil {
		t.Fatalf("ordinary row phase rejected: %v", err)
	}
}
