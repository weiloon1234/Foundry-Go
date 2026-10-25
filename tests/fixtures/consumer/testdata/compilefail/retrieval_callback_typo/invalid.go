package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"
import "foundry.test/consumer/retrievalqueries"

var _ = orm.RetrievalHooks[retrievalqueries.Member]{Retreived: nil}
