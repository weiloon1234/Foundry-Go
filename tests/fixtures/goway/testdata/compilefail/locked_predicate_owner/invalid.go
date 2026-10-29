package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

var _ = orm.Lock(models.Users.Query()).Where(orm.NumberGt(models.Orders.TotalCents, 1))
