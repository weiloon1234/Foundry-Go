package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.RangeBoundary[models.Order, int](expr.NumericRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.Select(expr.Value(models.Users.Age))).Preceding(1))
