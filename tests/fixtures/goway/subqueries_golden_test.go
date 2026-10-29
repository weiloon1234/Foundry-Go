package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/value"
)

type subqueryResult struct {
	Amount, Count value.Nullable[int64]
	Name          value.Nullable[string]
}
type subqueryFields struct {
	rowset.Row[subqueryResult]
	Amount, Count rowset.Field[subqueryResult, value.Nullable[int64]]
	Name          rowset.Field[subqueryResult, value.Nullable[string]]
}

var subqueryRows = rowset.Define(func(b rowset.Builder[subqueryResult], r *subqueryResult) subqueryFields {
	return subqueryFields{b.Row(), rowset.NullableNumberOf(b, &r.Amount), rowset.NullableNumberOf(b, &r.Count), rowset.NullableTextOf(b, &r.Name)}
})

func TestStage6b2SubqueryGolden(t *testing.T) {
	after := &goldenCapture{}
	for _, g := range []*goldenCapture{after} {
		g.scriptedColumns = []string{"amount", "count", "name"}
		g.scriptedBatches = [][][]driver.Value{{{nil, int64(0), nil}, {int64(30), int64(2), "nick"}}}
	}

	runSubqueryNew(after)
	if after.statements != 1 || !oracleEqual(t, "subqueries_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func runSubqueryNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	u, o := models.Users, models.Orders
	outer := rowset.From(u.Query().Where(orm.NumberGt(u.Age, 18)))
	amount := rowset.SelectScalar(rowset.From(o.Query().Where(orm.NumberGt(o.TotalCents, 5)).OrderBy(orm.NumberDesc(o.TotalCents)).Limit(1)), expr.Row(expr.Value(orm.NumberExpr(o.TotalCents))).Value())
	count := rowset.SelectScalar(rowset.From(o.Query()), expr.Count[models.Order]().Value())
	name := rowset.SelectScalar(rowset.From(u.Query().Where(orm.NumberLt(u.Age, 90)).Limit(1)), expr.Row(expr.Value(orm.NullableTextExpr(u.Nickname))).Value())
	ids := rowset.WhereScalar(rowset.SelectScalar(rowset.From(u.Query()), expr.Row(expr.Value(orm.KeyExpr(u.ID))).Value()), expr.OrderRow(expr.Value(orm.NumberExpr(u.Age))).Gt(20))
	age := rowset.SelectScalar(rowset.From(u.Query().Where(orm.NumberLt(u.Age, 99)).Limit(1)), expr.Row(expr.Value(orm.NumberExpr(u.Age))).Value())
	countZero := rowset.SelectScalar(rowset.From(o.Query().Limit(0)), expr.Count[models.Order]().Value())
	exists := rowset.As[joinC](o.Query().Where(orm.NumberGt(o.TotalCents, 3)), "exists_orders")
	s := subqueryRows
	q := rowset.OrderBy(rowset.Select(outer, s, rowset.MapSelected(s.Amount, rowset.ScalarRowQuery(outer, amount).Value()), rowset.MapSelected(s.Count, rowset.ScalarRowQuery(outer, count).Value()), rowset.MapSelected(s.Name, rowset.ScalarNullableRowQuery(outer, name).Value())).Where(rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), ids), expr.Not(rowset.InNullableQuery(expr.Value(orm.TextExpr(u.Email)), name)), rowset.ExistsQuery(outer, exists), expr.GreaterNullable(expr.NullableRow(expr.Value(orm.NumberExpr(u.Age))), rowset.ScalarRowQuery(outer, age)), expr.Not(rowset.ExistsQuery(outer, countZero))), expr.Value(orm.NumberExpr(u.Age)).Asc())
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(q).All(ctx, db))}))
}
