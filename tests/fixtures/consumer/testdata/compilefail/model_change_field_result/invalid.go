package invalid

import (
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid(c orm.Changes[mutatorqueries.Member]) {
	change, _ := orm.NullableChangeOf(c, mutatorqueries.Members.Nickname)
	var _ value.Optional[string] = change.After()
}
