package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var _, _ = orm.CreateEach(context.Background(), db, linkqueries.Memberships.Query(), [][]orm.Assignment[linkqueries.Member]{})
