package invalid

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ query.KeyField[n.Project, n.Slug] = n.TeamFields().Name
