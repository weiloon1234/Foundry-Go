package aggregate_filter_having

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Filter(expr.Sum(orm.NumberExpr(models.Orders.TotalCents)), expr.GtValue(expr.CountField(orm.NumberExpr(models.Orders.TotalCents)), 0))
