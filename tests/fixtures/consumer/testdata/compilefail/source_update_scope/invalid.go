package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(linkqueries.Members.Query(), rows.From(linkqueries.Archives.Query())).Select(rows.MapUpdate(linkqueries.Members.Name, expr.Value(linkqueries.Members.Name)))
