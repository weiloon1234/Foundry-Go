package invalid

import (
	"foundry.test/consumer/linkqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _, _ = orm.Compare(mutatorqueries.Members.Table, value.Set(mutatorqueries.Member{}), value.Set(mutatorqueries.Member{}), linkqueries.Members.Name.Set("wrong"))
