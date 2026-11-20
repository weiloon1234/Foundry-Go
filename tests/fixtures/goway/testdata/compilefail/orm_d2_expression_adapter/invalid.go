package invalid

import (
	"foundry.test/goway/models/sensitive"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Value(sensitive.Vaults.Token)
