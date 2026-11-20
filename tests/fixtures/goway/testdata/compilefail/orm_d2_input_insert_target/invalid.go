package invalid

import (
	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapInsert(mutators.Members.Email, expr.Value(mutators.Members.Email))
