package binary_scalar_key

import (
	c "foundry.test/goway/coremodels"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.Key[c.Record, []byte]
