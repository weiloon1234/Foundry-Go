package invalid

import (
	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ConflictSet(inputqueries.Members.Email, inputqueries.StoredEmail("stored"))
