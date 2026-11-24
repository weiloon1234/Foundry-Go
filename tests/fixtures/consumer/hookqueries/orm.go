package hookqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var memberFields MemberTable

func init() { memberFields = Members }

func memberHooks() orm.Hooks[Member] { return ormMemberHooks(memberFields) }
