package invalid

import (
	m "foundry.test/goway/models"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.InsertMany(nil, nil, m.Users.Table, [][]orm.Assignment[m.Order]{})
