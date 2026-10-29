package query_icontains_nullable

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
	value "github.com/weiloon1234/Foundry-Go/value"
)

var _ = orm.NullableTextIContains(m.Users.Nickname, value.Of("name"))
