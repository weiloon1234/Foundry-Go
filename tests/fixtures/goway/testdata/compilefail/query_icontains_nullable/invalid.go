package query_icontains_nullable

import (
	m "foundry.test/goway/models"
	value "github.com/weiloon1234/Foundry-Go/value"
)

var _ = m.Users.Nickname.IContains(value.Of("name"))
