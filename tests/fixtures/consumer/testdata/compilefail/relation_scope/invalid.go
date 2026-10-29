package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var invalid = models.UserLinks.Orders.Where(orm.NumberGt(models.Users.Age, 18))
