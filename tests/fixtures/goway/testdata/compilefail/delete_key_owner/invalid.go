package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
)

var _, _ = orm.Delete(nil, nil, m.Users.Table.Query(), m.Users.Table.Key(), m.Order{}.ID)
