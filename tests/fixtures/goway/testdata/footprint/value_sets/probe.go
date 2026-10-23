package value_sets

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Read(ctx context.Context, db *database.DB) ([]int64, error) {
	a := rows.Values(rows.SelectScalar(rows.From(models.Orders.Query()), expr.Select(expr.Value(models.Orders.TotalCents))))
	q := rows.ExceptAllValues(rows.UnionAllValues(a, a), rows.IntersectAllValues(a, a))
	return q.Where(q.Value().Ne(0)).OrderBy(q.Value().Asc()).Limit(10).All(ctx, db)
}
func Nullable(ctx context.Context, db *database.DB) ([]value.Nullable[string], error) {
	a := rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Nickname))))
	q := rows.UnionValues(a, a)
	return q.OrderBy(q.Value().Asc()).All(ctx, db)
}
