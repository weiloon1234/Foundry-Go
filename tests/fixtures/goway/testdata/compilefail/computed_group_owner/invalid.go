package computed_group_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.GroupBy(rows.SelectValue(models.Users.Query(), models.Users.Age), expr.Group(expr.Param(models.Orders.TotalCents, 1)))
