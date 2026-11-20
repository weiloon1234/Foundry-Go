package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.OrderByPivot(orm.Asc(j.Targets.Name))
