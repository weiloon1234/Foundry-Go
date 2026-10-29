package uuid_seeks

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
)

func Run(ctx context.Context, db database.Executor, after, through model.ID[models.Order]) ([]models.Order, error) {
	return orm.All(ctx, db, expr.Where(models.Orders.Query().Where(orm.UUIDGreater(orm.KeyExpr(models.Orders.ID), after)), expr.UUIDLessOrEqual(expr.Value(orm.KeyExpr(models.Orders.ID)), through)).OrderBy(orm.KeyAsc(models.Orders.ID)).Limit(100))
}
