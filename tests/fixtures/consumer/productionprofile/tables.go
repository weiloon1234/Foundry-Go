package productionprofile

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Records.Table} }
