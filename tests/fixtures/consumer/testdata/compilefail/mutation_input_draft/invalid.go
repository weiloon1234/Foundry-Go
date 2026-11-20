package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/inputqueries"
)

var _ = orm.Set(inputqueries.Members.Email, inputqueries.StoredEmail("stored"))
