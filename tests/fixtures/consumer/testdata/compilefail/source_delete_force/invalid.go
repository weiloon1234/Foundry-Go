package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.ForceDeleteUsing(linkqueries.Archives.Query(), linkqueries.Members.Soft, rows.From(linkqueries.Members.Query()))
