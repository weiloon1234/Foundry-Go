package derived_aggregate_selected

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.SumRow(expr.AsSelected(expr.Sum(orm.NumberExpr(models.Users.Age))))
