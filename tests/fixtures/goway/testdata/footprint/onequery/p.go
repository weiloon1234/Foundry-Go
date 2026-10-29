package onequery

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]models.Order, error) {
	return orm.All(ctx, db, models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 100)).Limit(10))
}
