package q_select

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() rows.Query[models.Order, int64] {
	return rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents)
}
