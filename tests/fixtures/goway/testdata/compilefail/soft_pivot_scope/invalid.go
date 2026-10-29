package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	j "foundry.test/goway/models/joined"
)

var _ = j.Peers.OnlyTrashedPivot().WherePivot(orm.TextEq(j.Parents.Name, "wrong model"))
