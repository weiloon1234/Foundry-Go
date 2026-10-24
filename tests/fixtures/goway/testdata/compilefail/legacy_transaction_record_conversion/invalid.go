package invalid

import "github.com/weiloon1234/Foundry-Go/database/query"

type record struct{ ID int64 }

func escape(source query.SetQuery[record]) {
	_ = query.SetQuery[record](query.TransactionOf(source))
}
