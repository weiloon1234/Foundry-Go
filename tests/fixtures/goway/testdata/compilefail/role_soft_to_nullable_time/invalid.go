package invalid

import (
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"time"
)

var _ = orm.NullableTime[h.Member, time.Time](h.Members.DeletedAt)
