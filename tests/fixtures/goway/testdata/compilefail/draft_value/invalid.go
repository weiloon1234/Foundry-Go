package draft_value

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
)

var _ = orm.Set(m.Users.Email, 42)
