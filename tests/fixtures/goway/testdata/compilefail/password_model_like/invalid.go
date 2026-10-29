package invalid

import (
	s "foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.TextLike(s.Accounts.Digest, "%hash%")
