package enum_ordered

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.NumberGt(models.Users.Status, models.StatusActive) }
