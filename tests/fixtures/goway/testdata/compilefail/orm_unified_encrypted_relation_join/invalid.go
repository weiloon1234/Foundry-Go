package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.JoinKeys(sensitive.Vaults.Token, sensitive.Vaults.Token)
