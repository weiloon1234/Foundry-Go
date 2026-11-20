package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapUpdate(h.Members.Name, expr.Value(h.Members.Name))
