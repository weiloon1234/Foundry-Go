package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Increment(orm.NumberKey(h.Members.Key()), 1)
