package invalid

import (
	"foundry.test/consumer/inputqueries"
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(write *orm.Write[inputqueries.Member]) {
	_ = orm.WriteUnset(write, observerqueries.Plains.Name.Set)
}
