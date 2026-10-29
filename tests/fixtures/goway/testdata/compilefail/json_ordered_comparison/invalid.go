package json_ordered_comparison

import (
	c "foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.NumberGt(c.Records.Settings, c.Record{}.Settings)
