package invalid

import (
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit/record"
)

func invalid() {
	_, _ = auditqueries.AccountAuditFields(record.Model[auditqueries.Label, auditqueries.LabelCode]{})
}
