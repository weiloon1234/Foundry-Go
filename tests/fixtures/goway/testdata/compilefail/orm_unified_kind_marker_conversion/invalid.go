package invalid

import "github.com/weiloon1234/Foundry-Go/database/orm"

var _ orm.RequiredTextKind = orm.RequiredTextKind(orm.RequiredEnumKind{})
