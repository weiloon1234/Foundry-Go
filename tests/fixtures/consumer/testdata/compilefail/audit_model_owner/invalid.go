package invalid

import (
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid() {
	_, _ = orm.AuditRead(record.Model[auditqueries.Label, auditqueries.LabelCode]{}, auditqueries.Accounts.Email)
}
