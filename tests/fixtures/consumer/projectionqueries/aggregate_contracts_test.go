package projectionqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestFilteredAggregatePhaseSQLParity(t *testing.T) {
	predicate := expr.ModelPredicate(orm.Gt(models.Orders.TotalCents, 0))
	got, err := rows.CompileRead(rows.Read(reports.FilteredOrderSummary()))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/projectionqueries/aggregate_contracts_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal(

			// Row predicates/orders use the same adapters as the phase-rejection negatives.
			"frozen legacy comparison differs")
	}

	row := expr.OrderBy(expr.Where(models.Orders.Query(), predicate), expr.Desc(expr.Value(models.Orders.TotalCents)))
	if _, err := orm.Compile(row); err != nil {
		t.Fatalf("ordinary row phase rejected: %v", err)
	}
}
