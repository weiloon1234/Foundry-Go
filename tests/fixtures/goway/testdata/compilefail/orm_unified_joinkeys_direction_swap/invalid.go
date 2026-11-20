package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var slot orm.Through[models.User, models.Order, models.WriteRecord]
var _ = orm.ManyToManyOf(slot, orm.JoinKeys(models.Orders.BuyerID, models.Users.ID), orm.JoinKeys(models.Users.ID, models.Orders.BuyerID))
