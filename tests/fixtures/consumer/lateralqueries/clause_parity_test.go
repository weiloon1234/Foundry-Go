package lateralqueries_test

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
)

func TestCorrelatedRecordClauseSQLParity(t *testing.T) {
	outer := rows.As[usersAlias](models.Users.Query(), "buyer")
	c, scope := userOrders(outer, rows.Scope(outer))
	count := expr.CountFieldAt(scope, models.Orders.ID)
	record := rows.ProjectCorrelated(c, reports.OrderSummaries, rows.MapSelected(reports.OrderSummaries.Total, expr.SumAt(scope, models.Orders.TotalCents).Value()), rows.MapSelected(reports.OrderSummaries.Count, count.Value()))
	record = rows.DistinctCorrelatedRecord(rows.HavingCorrelatedRecord(record, count.Gt(0)))
	lateral := rows.AsLateral[statsAlias](record, "stats")
	got, err := orm.Compile(expr.Where(models.Users.Query(), rows.ExistsQuery(rows.From(models.Users.Query()), rows.CrossJoinLateral(outer, lateral))))
	if err != nil {
		t.Fatal(err)
	}
	oldOuter := query.As[usersAlias](models.QueryUsers(), "buyer")
	oldOrders := query.As[ordersAlias](models.QueryOrders(), "orders_for_user")
	oldC := query.Correlate(oldOuter, oldOrders)
	u := models.UserFieldsAt(query.OuterScope(oldC, oldOuter.Scope()))
	o := models.OrderFieldsAt(query.InnerScope(oldC, oldOrders.Scope()))
	oldC = oldC.Where(query.Equal(o.BuyerID, u.ID))
	oldRecord := reports.ProjectCorrelatedOrderSummary(oldC).SelectTotal(query.Sum(o.TotalCents).Value()).SelectCount(query.CountField(o.ID).Value()).Query().Having(query.CountField(o.ID).Gt(0)).Distinct()
	oldLateral := query.AsLateral[statsAlias](oldRecord, "stats")
	want, err := models.QueryUsers().Where(query.ExistsQuery(models.QueryUsers(), query.CrossJoinLateral(oldOuter, oldLateral))).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/lateralqueries/clause_parity_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatalf("lateral clauses changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}
