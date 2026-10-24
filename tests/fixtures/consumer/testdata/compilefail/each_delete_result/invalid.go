package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	v, _ := orm.DeleteEach(ctx, db, linkqueries.Memberships.Query(), 10)
	var wrong []linkqueries.Archive = v
	_ = wrong
}
