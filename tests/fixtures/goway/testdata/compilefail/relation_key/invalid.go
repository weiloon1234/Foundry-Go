package relation_key

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.BelongsToOf(models.Orders.Buyer, models.Orders.BuyerID, models.Orders.ID) }
