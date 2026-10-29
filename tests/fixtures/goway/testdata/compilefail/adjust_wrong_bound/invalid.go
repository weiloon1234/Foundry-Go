package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var c orm.Number[h.Member, int64]
var _ = orm.NumberIncrement(c, 1, orm.AtMost(1.0))
