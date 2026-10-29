package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
	r "foundry.test/goway/reporting"
)

var _, _ = orm.Insert(nil, nil, m.Users.Table, r.OrderRow{})
