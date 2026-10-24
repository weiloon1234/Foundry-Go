package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapInsert(linkqueries.Archives.UpdatedAt, expr.Value(linkqueries.Memberships.UpdatedAt))
