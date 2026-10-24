package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var key orm.Key[h.Member, string]
var _ = orm.Text[h.Member, string](orm.TextKey(key))
