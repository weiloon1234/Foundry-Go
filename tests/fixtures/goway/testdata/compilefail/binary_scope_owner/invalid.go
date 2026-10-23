package binary_scope_owner

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
)

var _ = m.Users.Query().Where(c.Records.Body.Eq(c.Payload{1}))
