package invalid

import (
	"context"
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var row linkqueries.Member

func invalid() { row, _ = orm.FirstOrCreate(context.Background(), db, linkqueries.Memberships.Query()) }
