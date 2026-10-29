package invalid

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.Query[n.Project] = orm.Lock(n.ProjectRecords.Query())
