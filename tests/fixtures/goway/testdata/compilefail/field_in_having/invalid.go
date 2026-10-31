package field_in_having

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Having(rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents)), expr.Eq(expr.Value(orm.NumberExpr(models.Orders.TotalCents)), 1))
