package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

var _ = models.UserRelations.Orders.Where(orm.NumberGt(models.Users.Age, 18))
