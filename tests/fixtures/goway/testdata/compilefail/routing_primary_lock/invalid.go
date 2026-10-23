package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = models.Users.Query().Where(models.Users.ID.Eq(models.User{}.ID)).ForUpdate().Require(ctx, db.Primary())
}
