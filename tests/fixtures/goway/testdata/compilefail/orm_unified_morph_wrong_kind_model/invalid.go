package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var slot orm.Many[j.Parent, j.Target]
var _ = orm.MorphManyOf(slot, orm.JoinKeys(j.Parents.ID, j.Targets.ID), j.Pivots.Kind)
