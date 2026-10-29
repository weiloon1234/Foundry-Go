package aggregate_filter_nullable

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.Sum(orm.NumberExpr(models.Orders.TotalCents)).Filter(expr.Value(orm.NumberExpr(models.Orders.TotalCents)).Eq(0)).Gt(value.Of(decimal.FromInt64(1)))
