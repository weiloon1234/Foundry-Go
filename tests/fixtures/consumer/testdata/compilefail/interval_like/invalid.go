package invalid

import (
	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid() { _ = orm.TextLike(intervalqueries.Samples.Period, "%day%") }
