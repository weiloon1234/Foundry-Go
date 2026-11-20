package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.SelectValue(sensitive.Vaults.Query(), sensitive.Vaults.Token)
