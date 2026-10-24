package invalid

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.Update(ctx, db, models.Users.Query(), models.Users.Key(), "wrong", models.Users.Email.Set("Ada"))
}
