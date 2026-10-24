package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.When(expr.Value(models.Users.Age).Eq(0), expr.RowNumber(expr.WindowFor(rows.ModelScope(models.Users.Query()))))
