package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(linkqueries.Members.Query(), rows.From(linkqueries.Archives.Query())).Values(orm.TextSet(linkqueries.Archives.Name, "wrong"))
