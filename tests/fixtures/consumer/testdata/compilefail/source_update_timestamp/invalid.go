package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.MapUpdate(orm.TimeExpr(linkqueries.Memberships.UpdatedAt), expr.Value(orm.TimeExpr(linkqueries.Archives.UpdatedAt)))
