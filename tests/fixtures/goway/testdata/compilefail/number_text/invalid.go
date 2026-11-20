package number_text

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.Contains(models.Orders.TotalCents, "x") }
