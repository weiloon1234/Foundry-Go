package invalid

import (
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid(history record.Model[auditqueries.Account, model.ID[auditqueries.Account]]) {
	field, _ := orm.AuditRead(history, orm.InputTextExpr(auditqueries.Accounts.Email))
	var _ value.Optional[record.FieldChange[int]] = field
}
