package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func invalid(ctx context.Context, db database.Transactor) {
	var result []models.Order
	result, _ = rows.InsertFrom(rows.From(models.Users.Query()), models.Users.Query()).Returning(ctx, db, 1)
	_ = result
}
