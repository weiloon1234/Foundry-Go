package invalid

import (
	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapInsert(orm.InputTextExpr(mutators.Members.Email), expr.Value(orm.InputTextExpr(mutators.Members.Email)))
