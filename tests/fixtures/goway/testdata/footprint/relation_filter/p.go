package relation_filter

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func Run(ctx context.Context, ex database.Executor) ([]models.User, error) {
	r := models.UserRelations.Orders
	r = r.Where(expr.RelationFilter(r, expr.Gt(expr.OrderRow(expr.Value(orm.NumberExpr(models.Orders.TotalCents))), 100)))
	return orm.All(ctx, ex, models.Users.Query().Where(orm.Has(r)).With(r))
}
