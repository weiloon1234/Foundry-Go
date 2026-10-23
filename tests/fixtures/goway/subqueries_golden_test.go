package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, g := range []*goldenCapture{before, after} {
		g.scriptedColumns = []string{"amount", "count", "name"}
		g.scriptedBatches = [][][]driver.Value{{{nil, int64(0), nil}, {int64(30), int64(2), "nick"}}}
	}
	runSubqueryOld(before)
	runSubqueryNew(after)
	if before.statements != 1 || after.statements != 1 || before.output.String() != after.output.String() {
		t.Fatalf("subquery parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func runSubqueryOld(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	u, o := old.UserFields(), old.OrderFields()
	outer := old.QueryUsers().Where(u.Age.Gt(18))
	amount := query.SelectValue(old.QueryOrders().Where(o.TotalCents.Gt(5)).OrderBy(o.TotalCents.Desc()).Limit(1), query.Value(o.TotalCents))
	count := query.SelectValue(old.QueryOrders(), query.Count[old.Order]().Value())
	name := query.SelectValue(old.QueryUsers().Where(u.Age.Lt(90)), query.Value(u.Nickname)).Limit(1)
	ids := query.SelectValue(old.QueryUsers().Where(u.Age.Gt(20)), query.Value(u.ID))
	age := query.SelectValue(old.QueryUsers().Where(u.Age.Lt(99)), query.Value(u.Age)).Limit(1)
	a, c, n := query.NewProjectionField[subqueryResult, value.Nullable[int64]]("amount"), query.NewProjectionField[subqueryResult, value.Nullable[int64]]("count"), query.NewProjectionField[subqueryResult, value.Nullable[string]]("name")
	schema := query.DefineProjection([]query.ProjectionColumn[subqueryResult]{a.Column(), c.Column(), n.Column()}, func(row database.Row) (subqueryResult, error) {
		var r subqueryResult
		err := row.Scan(codec.Nullable(codec.Signed[int64]()).Scan(&r.Amount), codec.Nullable(codec.Signed[int64]()).Scan(&r.Count), codec.Nullable(codec.String[string]()).Scan(&r.Name))
		return r, err
	})
	exists := query.As[joinC](old.QueryOrders().Where(o.TotalCents.Gt(3)), "exists_orders")
	q := query.Project(outer, schema, query.Map(a, query.ScalarQuery(outer, amount)), query.Map(c, query.ScalarQuery(outer, count)), query.Map(n, query.ScalarNullableQuery(outer, name))).Where(u.ID.InQuery(ids), u.Email.InNullableQuery(name).Not(), query.ExistsQuery(outer, exists), query.GreaterNullable(query.NullableRow(u.Age), query.ScalarRowQuery(outer, age)), query.ExistsQuery(outer, count.Limit(0)).Not()).OrderBy(u.Age.Asc())
	fmt.Fprintln(&g.output, must(q.All(ctx, db)))
}
func runSubqueryNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	u, o := models.Users, models.Orders
	outer := rowset.From(u.Query().Where(u.Age.Gt(18)))
	amount := rowset.SelectScalar(rowset.From(o.Query().Where(o.TotalCents.Gt(5)).OrderBy(o.TotalCents.Desc()).Limit(1)), expr.Select(expr.Value(o.TotalCents)))
	count := rowset.SelectScalar(rowset.From(o.Query()), expr.Count[models.Order]().Value())
	name := rowset.SelectScalar(rowset.From(u.Query().Where(u.Age.Lt(90)).Limit(1)), expr.Select(expr.Value(u.Nickname)))
	ids := rowset.WhereScalar(rowset.SelectScalar(rowset.From(u.Query()), expr.Select(expr.Value(u.ID))), expr.OrderRow(expr.Value(u.Age)).Gt(20))
	age := rowset.SelectScalar(rowset.From(u.Query().Where(u.Age.Lt(99)).Limit(1)), expr.Select(expr.Value(u.Age)))
	countZero := rowset.SelectScalar(rowset.From(o.Query().Limit(0)), expr.Count[models.Order]().Value())
	exists := rowset.As[joinC](o.Query().Where(o.TotalCents.Gt(3)), "exists_orders")
	s := subqueryRows
	q := rowset.Select(outer, s, rowset.MapSelected(s.Amount, rowset.ScalarQuery(outer, amount)), rowset.MapSelected(s.Count, rowset.ScalarQuery(outer, count)), rowset.MapSelected(s.Name, rowset.ScalarNullableQuery(outer, name))).Where(rowset.InQuery(expr.Value(u.ID), ids), expr.Not(rowset.InNullableQuery(expr.Value(u.Email), name)), rowset.ExistsQuery(outer, exists), expr.GreaterNullable(expr.NullableRow(expr.Value(u.Age)), rowset.ScalarRowQuery(outer, age)), expr.Not(rowset.ExistsQuery(outer, countZero))).OrderBy(expr.Value(u.Age).Asc())
	fmt.Fprintln(&g.output, must(q.All(ctx, db)))
}
