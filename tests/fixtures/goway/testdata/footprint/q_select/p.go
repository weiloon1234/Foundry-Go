package q_select

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() rows.Query[models.Order, int64] {
	return rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents))
}
