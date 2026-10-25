package derived_aggregate_selected

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.SumRow(expr.Sum(models.Users.Age).Value())
