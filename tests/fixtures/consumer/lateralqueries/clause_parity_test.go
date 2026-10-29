package lateralqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestCorrelatedRecordClauseSQLParity(t *testing.T) {
	outer := rows.As[usersAlias](models.Users.Query(), "buyer")
	c, scope := userOrders(outer, rows.Scope(outer))
	count := expr.CountFieldAt(scope, orm.KeyExpr(models.Orders.ID))
	record := rows.ProjectCorrelated(c, reports.OrderSummaries, rows.MapSelected(reports.OrderSummaries.Total, expr.SumAt(scope, orm.NumberExpr(models.Orders.TotalCents)).Value()), rows.MapSelected(reports.OrderSummaries.Count, count.Value()))
	record = rows.DistinctCorrelatedRecord(rows.HavingCorrelatedRecord(record, count.Gt(0)))
	lateral := rows.AsLateral[statsAlias](record, "stats")
	got, err := orm.Compile(expr.Where(models.Users.Query(), rows.ExistsQuery(rows.From(models.Users.Query()), rows.CrossJoinLateral(outer, lateral))))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/lateralqueries/clause_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
}
