package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
)

func bad() { items, _ := orm.InsertMany(nil, nil, m.Users.Table, nil); var _ []m.Order = items }
