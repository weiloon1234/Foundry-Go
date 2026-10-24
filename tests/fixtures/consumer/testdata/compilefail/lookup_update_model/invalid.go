package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var _, _ = orm.UpdateOrCreate(context.Background(), db, linkqueries.Memberships.Query(), []orm.Assignment[linkqueries.Membership]{}, func(context.Context, *database.Tx, linkqueries.Member) ([]orm.Assignment[linkqueries.Membership], error) {
	return nil, nil
})
