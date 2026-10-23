package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.HasManyThroughOf(j.Parents.Targets, j.Parents.ID, j.Pivots.ParentID, j.Targets.ParentID, j.Targets.ID)
