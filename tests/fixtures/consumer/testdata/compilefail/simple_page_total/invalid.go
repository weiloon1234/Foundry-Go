package invalid

import "github.com/weiloon1234/Foundry-Go/database/query"

var _ = query.SimplePage[int]{}.Total
