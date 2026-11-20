package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Decrement(orm.NumberKey(h.Members.ID), 1)
