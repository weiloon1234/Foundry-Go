package nested_field_owner

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.Key[m.User, c.Code] = c.Records.Code
