package invalid

import (
	"context"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Hooks[softqueries.Member]{Restoring: func(context.Context, *database.Tx, softqueries.Membership) error { return nil }}
