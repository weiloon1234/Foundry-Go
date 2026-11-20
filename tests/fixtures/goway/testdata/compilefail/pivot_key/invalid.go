package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ManyToManyOf(j.Parents.Peers, orm.JoinKeys(j.Parents.ID, j.Pivots.ParentID), orm.JoinKeys(j.Pivots.Kind, j.Targets.ID))
