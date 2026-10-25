package invalid

import (
	"context"
	"foundry.test/consumer/hookqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Hooks[hookqueries.Member]{Deleting: func(context.Context, *database.Tx, mutatorqueries.Member) error { return nil }}
