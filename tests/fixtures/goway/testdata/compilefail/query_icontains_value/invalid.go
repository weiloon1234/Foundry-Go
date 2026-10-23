package query_icontains_value

import (
	m "foundry.test/goway/models"
)

var _ = m.Users.Email.IContains(42)
