package interval_draft_text

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
)

var _ = orm.IntervalSet(c.Records.Period, "1 day")
