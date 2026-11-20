package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.MorphToOf(j.Pivots.MorphParent, orm.JoinKeys(j.Pivots.ParentID, j.Targets.ID), j.Pivots.Kind)
