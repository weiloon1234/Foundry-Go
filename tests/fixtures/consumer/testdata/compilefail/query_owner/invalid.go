package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var invalid = models.Users.Query().Where(orm.NumberEq(models.Orders.TotalCents, 10))
