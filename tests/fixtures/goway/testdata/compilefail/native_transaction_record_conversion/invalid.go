package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

type record struct{ ID int64 }

func escape(source rows.SetQuery[record]) {
	_ = rows.SetQuery[record](rows.TransactionOf(source))
}
