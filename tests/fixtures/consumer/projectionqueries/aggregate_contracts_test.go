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
	total := expr.Sum(orm.NumberExpr(models.Orders.TotalCents)).Filter(predicate)
	count := expr.Count[models.Order]().Filter(predicate)
	exists := expr.Exists[models.Order]().Filter(predicate)
	selected := rows.Select(rows.From(models.Orders.Query()), reports.OrderSummaries,
		rows.MapSelected(reports.OrderSummaries.Total, total.Value()),
		rows.MapSelected(reports.OrderSummaries.Count, count.Value()))
	got, err := rows.CompileRead(rows.Read(rows.Having(selected, expr.HavingAnd(count.Gt(0), total.Gt(decimal.FromInt64(1)), exists.Eq(true)))))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/projectionqueries/aggregate_contracts_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal(

			// Row predicates/orders use the same adapters as the phase-rejection negatives.
			"frozen legacy comparison differs")
	}

	row := expr.OrderBy(expr.Where(models.Orders.Query(), predicate), expr.Value(orm.NumberExpr(models.Orders.TotalCents)).Desc())
	if _, err := orm.Compile(row); err != nil {
		t.Fatalf("ordinary row phase rejected: %v", err)
	}
}
