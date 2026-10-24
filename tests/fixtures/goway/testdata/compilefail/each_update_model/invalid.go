package invalid

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.UpdateEach(ctx, db, models.Orders.Query(), 10, func(context.Context, *database.Tx, models.User) ([]orm.Assignment[models.Order], error) {
		return nil, nil
	})
}
