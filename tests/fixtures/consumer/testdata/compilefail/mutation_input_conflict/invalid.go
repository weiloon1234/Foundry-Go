package invalid

import (
	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.ConflictSet(orm.InputTextExpr(inputqueries.Members.Email), inputqueries.StoredEmail("stored"))
