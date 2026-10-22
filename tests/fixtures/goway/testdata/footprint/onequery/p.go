package onequery

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]models.Order, error) {
	return models.Orders.Query().Where(models.Orders.TotalCents.Gt(100)).Limit(10).All(ctx, db)
}
