package conflict_value

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/consumer/models"
)

var _ = orm.Set(m.Users.Age, "thirty")
