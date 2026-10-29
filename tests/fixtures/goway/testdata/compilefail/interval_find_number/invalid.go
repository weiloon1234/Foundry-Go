package interval_find_number

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
)

var _, _ = orm.Find(nil, nil, c.Durations.Table, 1)
