package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"time"

	"foundry.test/consumer/timequeries"
)

var _ = orm.Set(timequeries.Members.UpdatedAt, time.Time{})
