package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.HasManyThroughOf(j.Parents.Targets, orm.KeyExpr(j.Parents.ID), orm.NullableNumberExpr(j.Pivots.ParentID), orm.NumberExpr(j.Targets.ParentID), orm.KeyExpr(j.Targets.ID))
