package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

var _ = models.OrderRelations.Buyer.Where(orm.NumberGt(models.Orders.TotalCents, 1))
