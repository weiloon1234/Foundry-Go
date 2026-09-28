package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = models.QueryUsers().ForUpdate().All(ctx, db.Primary())
}
