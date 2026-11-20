package binary_comparison_type

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
)

var _ = orm.Eq(c.Records.Body, "text")
