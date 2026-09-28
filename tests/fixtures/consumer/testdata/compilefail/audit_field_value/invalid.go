package invalid

import (
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ value.Optional[record.FieldChange[int]] = (auditqueries.AccountAuditFieldSet{}).Email
