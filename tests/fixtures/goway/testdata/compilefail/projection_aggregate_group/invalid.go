package projection_aggregate_group

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.AsSelected(expr.Sum(orm.NumberExpr(models.Orders.TotalCents))).Group()
