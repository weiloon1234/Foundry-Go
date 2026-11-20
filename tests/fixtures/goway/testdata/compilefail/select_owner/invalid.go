package select_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() { rows.SelectValue(models.Orders.Query(), models.Users.Age) }
