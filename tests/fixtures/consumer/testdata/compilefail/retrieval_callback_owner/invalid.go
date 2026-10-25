package invalid

import (
	"context"
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.RetrievalHooks[retrievalqueries.Member]{
	Retrieved: func(context.Context, database.Executor, retrievalqueries.Group) error { return nil },
}
