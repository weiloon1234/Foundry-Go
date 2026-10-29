package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.InsertFrom(rows.From(linkqueries.Members.Query()), linkqueries.Archives.Query()).Values(orm.TextSet(linkqueries.Members.Name, "wrong"))
