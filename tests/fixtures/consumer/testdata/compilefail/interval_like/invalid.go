package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid() { _ = orm.Like(intervalqueries.Samples.Period, "%day%") }
