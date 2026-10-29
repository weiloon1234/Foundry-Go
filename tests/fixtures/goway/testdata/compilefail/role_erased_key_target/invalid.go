package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var key orm.KeyColumn[h.Member, int64] = h.Members.ID
var _ = rows.MapUpdate(key, expr.Value(orm.KeyExpr(h.Members.ID)))
