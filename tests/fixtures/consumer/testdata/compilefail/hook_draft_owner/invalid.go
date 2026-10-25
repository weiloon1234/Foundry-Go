package invalid

import (
	"context"
	"foundry.test/consumer/hookqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Hooks[hookqueries.Member]{Creating: func(context.Context, *database.Tx, *orm.Write[mutatorqueries.Member]) error { return nil }}
