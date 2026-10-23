package invalid

import (
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
	temporal "github.com/weiloon1234/Foundry-Go/temporal"
	time "time"
)

type M struct{}

var field orm.UpdatedAt[M, temporal.DateTime]
var _ = field.Set(time.Time{})
