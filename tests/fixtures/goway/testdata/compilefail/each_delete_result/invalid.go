package invalid

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	v, _ := orm.DeleteEach(ctx, db, models.Orders.Query(), 10)
	var wrong []models.User = v
	_ = wrong
}
