package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Auditing(h.Members.Table, orm.AuditPolicy[h.KeyedMember]{})
