package interval_adjustment

import (
	c "foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.NumberIncrement(c.Records.Period, c.Record{}.Period)
