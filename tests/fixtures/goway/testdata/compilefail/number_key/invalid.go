package number_key

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.NumberKey(models.Orders.Key()) }
