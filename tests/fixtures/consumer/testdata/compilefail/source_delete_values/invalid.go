package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.DeleteUsing(linkqueries.Members.Query(), rows.From(linkqueries.Archives.Query())).Values(linkqueries.Members.Name.Set("wrong"))
