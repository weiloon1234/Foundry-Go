package invalid

import (
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

var _ = relation.Link[j.Target, j.Pivot]{Model: j.Target{}, Pivot: j.Parent{}}
