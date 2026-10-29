package find_natural_key

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
)

var _, _ = orm.Find(nil, nil, c.Records.Table, m.StatusActive)
