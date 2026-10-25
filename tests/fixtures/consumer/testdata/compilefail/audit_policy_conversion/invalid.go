package invalid

import (
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.AuditPolicy[observerqueries.Plain](orm.AuditPolicy[observerqueries.Record]{})
