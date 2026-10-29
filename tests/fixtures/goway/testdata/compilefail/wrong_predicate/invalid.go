package wrong_predicate

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.Query().Where(orm.NumberGt(models.Users.Age, 1)) }
