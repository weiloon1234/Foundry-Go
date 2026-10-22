package relation_local_model

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.BelongsToOf(models.Orders.Buyer, models.Users.ID, models.Users.ID) }
