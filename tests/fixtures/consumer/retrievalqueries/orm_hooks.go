package retrievalqueries

import context "context"
import database "github.com/weiloon1234/Foundry-Go/database"

import "github.com/weiloon1234/Foundry-Go/database/orm"

func ormMemberRetrieval(fields MemberTable) orm.RetrievalHooks[Member] {
	return orm.RetrievalHooks[Member]{Retrieved: memberRetrieved(func() func(arg0 context.Context, arg1 database.Executor) (int64, error) {
		receiver := fields.Query()
		return func(arg0 context.Context, arg1 database.Executor) (int64, error) {
			return orm.Count(arg0, arg1, receiver)
		}
	}())}
}
