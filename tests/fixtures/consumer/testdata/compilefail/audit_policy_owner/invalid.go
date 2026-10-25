package invalid

import (
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Auditing(auditqueries.Accounts.Table, auditqueries.LabelPolicy)
