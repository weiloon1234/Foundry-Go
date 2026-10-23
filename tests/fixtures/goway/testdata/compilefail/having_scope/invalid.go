package having_scope

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Having(rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents), expr.Count[models.User]().Gt(1))
