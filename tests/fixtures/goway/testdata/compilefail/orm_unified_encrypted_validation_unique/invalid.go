package invalid

import (
	"foundry.test/goway/models/sensitive"
	dbvalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

var _ = dbvalidation.Unique(nil, sensitive.Vaults.Query(), sensitive.Vaults.Token)
