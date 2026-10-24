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
	return expr.Where(models.Orders.Query().Where(orm.UUIDGreater(models.Orders.ID, after)), expr.UUIDLessOrEqual(expr.Value(models.Orders.ID), through)).OrderBy(models.Orders.ID.Asc()).Limit(100).All(ctx, db)
}
