package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ConflictKey(sensitive.Vaults.Token)
