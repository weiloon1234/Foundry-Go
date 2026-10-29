package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Sum(orm.NumberExpr(models.Orders.TotalCents)).Filter(expr.CountField(orm.NumberExpr(models.Orders.TotalCents)).Gt(0))
