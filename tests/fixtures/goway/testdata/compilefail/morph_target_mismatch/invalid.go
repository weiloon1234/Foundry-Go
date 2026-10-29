package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.MorphToOf(j.Pivots.MorphParent, orm.NullableNumberExpr(j.Pivots.ParentID), orm.ColumnExpr(j.Pivots.Kind), orm.KeyExpr(j.Targets.ID))
