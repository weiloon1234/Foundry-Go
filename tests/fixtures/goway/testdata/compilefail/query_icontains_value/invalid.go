package query_icontains_value

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
)

var _ = orm.TextIContains(m.Users.Email, 42)
