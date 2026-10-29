package invalid

import (
	"foundry.test/consumer/inputqueries"
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(write *orm.Write[inputqueries.Member]) {
	_ = orm.WriteUnset(write, func() func(arg0 string) orm.Assignment[observerqueries.Plain] {
		receiver := observerqueries.Plains.Name
		return func(arg0 string) orm.Assignment[observerqueries.Plain] { return orm.TextSet(receiver, arg0) }
	}())
}
