package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.Over(expr.Count[models.Order](), expr.WindowFor(rows.ModelScope(models.Users.Query())).Named("users"))
