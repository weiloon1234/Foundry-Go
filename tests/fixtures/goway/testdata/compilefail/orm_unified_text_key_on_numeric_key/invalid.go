package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.TextKey(scalars.Records.Key())
