package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DeleteUsing(linkqueries.Members.Query(), rows.From(linkqueries.Archives.Query())).Values(orm.Set(linkqueries.Members.Name, "wrong"))
