package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.InsertFrom(rows.From(linkqueries.Members.Query()), linkqueries.Archives.Query()).Select(rows.MapInsert(linkqueries.Archives.Name, expr.Value(linkqueries.Groups.Name)))
