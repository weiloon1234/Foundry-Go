package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(history record.Model[h.KeyedMember, int64]) {
	_, _ = orm.AuditRead(history, h.Members.Name)
}
