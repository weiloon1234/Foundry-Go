package invalid

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.Query[n.Project] = n.ProjectRecords.Query().ForUpdate()
