package invalid

import "github.com/weiloon1234/Foundry-Go/database/migrate"

const identifier migrate.ID = "20260911000000_records"

// Migration identity fields retain their distinct semantic types.
var key = migrate.Key{Origin: identifier, ID: identifier}
