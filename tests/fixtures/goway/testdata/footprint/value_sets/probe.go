package value_sets

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Read(ctx context.Context, db *database.DB) ([]int64, error) {
	a := rows.Values(rows.SelectScalar(rows.From(models.Orders.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Orders.TotalCents))))))
	q := rows.ExceptAllValues(rows.UnionAllValues(a, a), rows.IntersectAllValues(a, a))
	return rows.ReadValues(q.Where(expr.Ne(q.Value(), 0)).OrderBy(expr.Asc(q.Value())).Limit(10)).All(ctx, db)
}
func Nullable(ctx context.Context, db *database.DB) ([]value.Nullable[string], error) {
	a := rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))))))
	q := rows.UnionValues(a, a)
	return rows.ReadValues(q.OrderBy(expr.Asc(q.Value()))).All(ctx, db)
}
