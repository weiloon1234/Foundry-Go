package compilefail

import "github.com/weiloon1234/Foundry-Go/database"

var endpoint string
var _ = database.PoolHealth{Role: endpoint}
