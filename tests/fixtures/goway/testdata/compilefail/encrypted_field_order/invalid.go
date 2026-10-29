package invalid

import (
	s "foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.TextAsc(s.Vaults.Token)
