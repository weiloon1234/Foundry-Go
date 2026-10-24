package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var c orm.InputNumber[h.Member, int64, int64]
var _ = c.Increment(1)
