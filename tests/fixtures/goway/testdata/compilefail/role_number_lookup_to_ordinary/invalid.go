package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var key orm.Key[h.Member, int64]
var _ = orm.Number[h.Member, int64](orm.NumberKey(key))
