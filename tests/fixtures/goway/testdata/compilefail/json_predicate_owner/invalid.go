package json_predicate_owner

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
	value "github.com/weiloon1234/Foundry-Go/value"
)

var _ = m.Users.Query().Where(c.Records.Settings.Eq(value.JSON[c.Preferences]{}))
