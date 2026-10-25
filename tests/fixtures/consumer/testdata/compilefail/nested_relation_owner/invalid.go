package invalid

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.DirectRelation[n.Team, n.Task] = n.TeamLinks.Projects
