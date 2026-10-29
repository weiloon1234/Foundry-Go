package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var invalid = models.Users.Query().OrderBy(orm.KeyDesc(models.Orders.ID))
