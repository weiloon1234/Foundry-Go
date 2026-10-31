package aggregate_filter_boolean_order

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.GtValue(expr.Filter(expr.Exists[models.Order](), expr.Eq(expr.Value(orm.NumberExpr(models.Orders.TotalCents)), 0)), true)
