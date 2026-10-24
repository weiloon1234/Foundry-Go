package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.WindowFor(rows.ModelScope(models.Users.Query())).RangeBetween(expr.Preceding(1), expr.CurrentRow())
