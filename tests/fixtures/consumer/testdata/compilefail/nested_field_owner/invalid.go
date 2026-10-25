package invalid

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.KeyColumn[n.Project, n.Slug] = n.TeamRecords.Name
