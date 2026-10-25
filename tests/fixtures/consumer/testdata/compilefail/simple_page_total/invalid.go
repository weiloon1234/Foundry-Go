package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

var _ = orm.SimplePage[int]{}.Total
