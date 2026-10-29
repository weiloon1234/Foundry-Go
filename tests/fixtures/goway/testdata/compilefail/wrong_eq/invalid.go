package wrong_eq

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

func F() { orm.NumberEq(models.Orders.TotalCents, "x") }
