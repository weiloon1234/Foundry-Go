package lateral

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type parentTag struct{}
type childTag struct{}
type latestTag struct{}
type result struct{ Amount value.Nullable[int64] }
type fields struct {
	rows.Row[result]
	Amount rows.Field[result, value.Nullable[int64]]
}
type total struct{ Count int64 }
type totalFields struct {
	rows.Row[total]
	Count rows.Field[total, int64]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NullableNumberOf(b, &r.Amount)}
})
var totals = rows.Define(func(b rows.Builder[total], r *total) totalFields {
	return totalFields{b.Row(), rows.NumberOf(b, &r.Count)}
})

func F() rows.Query[rows.Left[rows.Alias[parentTag, models.User], rows.Alias[latestTag, models.Order]], result] {
	parent := rows.As[parentTag](models.Users.Query(), "parent")
	child := rows.As[childTag](models.Orders.Query(), "child")
	c := rows.Correlate(parent, child)
	is := rows.InnerScope(c, rows.Scope(child))
	os := rows.OuterScope(c, rows.Scope(parent))
	c = c.Where(expr.Equal(expr.At(is, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(os, orm.KeyExpr(models.Users.ID))))
	record := rows.SelectCorrelatedRecord(c, is).OrderBy(expr.Desc(expr.At(is, orm.NumberExpr(models.Orders.TotalCents)))).Limit(1)
	latest := rows.AsLateral[latestTag](record, "latest")
	joined := rows.LeftJoinLateral(parent, latest)
	amount := expr.AtNullable(rows.NullableRightScope(joined, rows.LateralScope(latest)), orm.NumberExpr(models.Orders.TotalCents))
	return rows.Select(joined, schema, rows.Map(schema.Amount, amount))
}

func Projected() rows.Query[rows.Left[rows.Alias[parentTag, models.User], rows.Alias[latestTag, total]], result] {
	parent := rows.As[parentTag](models.Users.Query(), "parent")
	child := rows.As[childTag](models.Orders.Query(), "child")
	c := rows.Correlate(parent, child)
	record := rows.ProjectCorrelated(c, totals, rows.MapSelected(totals.Count, expr.AsSelected(expr.Count[rows.Correlation[rows.Alias[parentTag, models.User], rows.Alias[childTag, models.Order]]]())))
	latest := rows.AsLateral[latestTag](record, "totals")
	joined := rows.LeftJoinLateral(parent, latest)
	count := rows.AtNullable(rows.NullableRightScope(joined, rows.LateralScope(latest)), totals.Count)
	return rows.Select(joined, schema, rows.MapSelected(schema.Amount, expr.AsSelected(expr.Row(count))))
}
