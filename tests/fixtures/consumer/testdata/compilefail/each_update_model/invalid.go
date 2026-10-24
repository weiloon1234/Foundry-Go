package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	_, _ = orm.UpdateEach(ctx, db, linkqueries.Memberships.Query(), 10, func(context.Context, *database.Tx, linkqueries.Archive) ([]orm.Assignment[linkqueries.Membership], error) {
		return nil, nil
	})
}
