package relation_key

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() {
	orm.BelongsToOf(models.Orders.Buyer, orm.ColumnExpr(models.Orders.BuyerID), orm.KeyExpr(models.Orders.ID))
}
