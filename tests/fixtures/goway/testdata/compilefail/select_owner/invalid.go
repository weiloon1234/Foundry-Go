package select_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() { rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Users.Age)) }
