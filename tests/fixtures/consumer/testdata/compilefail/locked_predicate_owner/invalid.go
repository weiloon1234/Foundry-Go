package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var _ = orm.Lock(models.Users.Query()).Where(orm.NumberGt(models.Orders.TotalCents, 1))
