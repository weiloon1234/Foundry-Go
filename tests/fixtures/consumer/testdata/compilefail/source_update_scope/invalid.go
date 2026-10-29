package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(linkqueries.Members.Query(), rows.From(linkqueries.Archives.Query())).Select(rows.MapUpdate(orm.TextExpr(linkqueries.Members.Name), expr.Value(orm.TextExpr(linkqueries.Members.Name))))
