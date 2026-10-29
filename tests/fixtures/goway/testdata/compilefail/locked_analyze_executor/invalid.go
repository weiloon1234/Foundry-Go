package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var ex database.Executor
var _, _ = orm.ExplainAnalyzeLocked(context.Background(), ex, orm.Lock(h.Members.Query()))
