package idempotenthttp

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Tables lists declarations for startup validation. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Workspaces, Orders} }
