package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/inputqueries"
)

var _ = orm.InputTextSet(inputqueries.Members.Email, inputqueries.StoredEmail("stored"))
