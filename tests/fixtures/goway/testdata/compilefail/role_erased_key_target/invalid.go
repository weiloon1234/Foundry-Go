package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var key orm.Key[h.Member, int64] = h.Members.Key()
var _ = rows.MapUpdate(key, expr.Value(h.Members.ID))
