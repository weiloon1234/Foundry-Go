package json_predicate_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
	value "github.com/weiloon1234/Foundry-Go/value"
)

var _ = m.Users.Query().Where(orm.JSONEq(c.Records.Settings, value.JSON[c.Preferences]{}))
