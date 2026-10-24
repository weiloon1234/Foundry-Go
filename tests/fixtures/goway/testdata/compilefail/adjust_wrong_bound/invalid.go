package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var c orm.Number[h.Member, int64]
var _ = c.Increment(1, orm.AtMost(1.0))
