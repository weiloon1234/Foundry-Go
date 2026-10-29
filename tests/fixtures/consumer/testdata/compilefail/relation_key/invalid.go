package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = orm.BelongsToOf(models.Orders.Buyer, orm.ColumnExpr(models.Orders.BuyerID), orm.KeyExpr(models.Orders.ID))
