package field_in_having

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Having(rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents), expr.Value(models.Orders.TotalCents).Eq(1))
