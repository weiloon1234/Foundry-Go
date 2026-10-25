package retrievalqueries

import "github.com/weiloon1234/Foundry-Go/database/orm"

func ormMemberRetrieval(fields MemberTable) orm.RetrievalHooks[Member] {
	return orm.RetrievalHooks[Member]{Retrieved: memberRetrieved(fields.Query().Count)}
}
