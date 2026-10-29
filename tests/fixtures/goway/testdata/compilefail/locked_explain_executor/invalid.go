package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var _, _ = orm.ExplainLocked(context.Background(), db, orm.Lock(h.Members.Query()))
