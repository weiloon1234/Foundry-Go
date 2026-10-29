package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.RequireLocked(ctx, db.Primary(), orm.Lock(models.Users.Query().Where(orm.KeyEq(models.Users.ID, models.User{}.ID))))
}
