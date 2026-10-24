package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.UpdateEach(ctx, db, linkqueries.Memberships.Query(), 10, func(context.Context, *database.Tx, linkqueries.Membership) ([]orm.Assignment[linkqueries.Archive], error) {
		return nil, nil
	})
}
