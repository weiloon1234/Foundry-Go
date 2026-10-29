package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapUpdate(orm.TimeExpr(h.Members.UpdatedAt), expr.Value(orm.TimeExpr(h.Members.UpdatedAt)))
