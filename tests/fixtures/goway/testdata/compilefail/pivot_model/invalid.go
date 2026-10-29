package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ManyToManyOf(j.Parents.Peers, orm.KeyExpr(j.Parents.ID), orm.NullableNumberExpr(j.Pivots.ParentID), orm.KeyExpr(j.Parents.ID), orm.KeyExpr(j.Targets.ID))
