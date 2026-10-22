package relation_nullable_model

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.BelongsToOf(models.Orders.Buyer, models.Users.IntroducerID, models.Users.ID) }
