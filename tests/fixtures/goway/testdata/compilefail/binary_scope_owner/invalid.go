package binary_scope_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
)

var _ = m.Users.Query().Where(orm.Eq(c.Records.Body, c.Payload{1}))
