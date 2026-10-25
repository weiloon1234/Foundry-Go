package invalid

import (
	"context"
	"foundry.test/consumer/hookqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Hooks[hookqueries.Member]{Updated: func(context.Context, *database.Tx, orm.Changes[mutatorqueries.Member]) error { return nil }}
