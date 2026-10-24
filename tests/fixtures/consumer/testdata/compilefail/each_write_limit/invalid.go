package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.DeleteEach(ctx, db, linkqueries.Memberships.Query(), "10")
}
