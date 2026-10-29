package compilefail

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.AllLocked(ctx, db.Primary(), orm.Lock(models.Users.Query()))
}
