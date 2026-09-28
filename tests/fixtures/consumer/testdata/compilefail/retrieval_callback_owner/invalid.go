package invalid

import (
	"context"
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _ = retrievalqueries.MemberRetrievalHooks{
	Retrieved: func(context.Context, database.Executor, retrievalqueries.Group) error { return nil },
}
