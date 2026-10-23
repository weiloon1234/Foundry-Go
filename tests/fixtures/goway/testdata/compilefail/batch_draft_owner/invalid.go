package invalid

import (
	m "foundry.test/goway/models"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = m.Users.InsertMany(nil, nil, [][]orm.Assignment[m.Order]{})
