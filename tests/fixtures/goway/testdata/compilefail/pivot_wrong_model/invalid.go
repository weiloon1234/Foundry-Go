package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid() { var _ orm.ThroughRelation[j.Parent, j.Target, j.Parent] = j.Peers }
