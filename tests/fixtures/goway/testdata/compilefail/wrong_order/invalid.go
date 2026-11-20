package wrong_order

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.Query().OrderBy(orm.Desc(models.Users.Age)) }
