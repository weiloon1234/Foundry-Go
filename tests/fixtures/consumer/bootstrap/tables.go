package bootstrap

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Members.Table, Operators.Table} }

var MemberIdentity = orm.IdentityOf(Members.Table, Members.ID)
var OperatorIdentity = orm.IdentityOf(Operators.Table, Operators.ID)
