package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid orm.HasOne[models.User, models.User] = orm.HasOneOf(models.Users.SingleOrder, models.Users.ID, models.Orders.BuyerID)
