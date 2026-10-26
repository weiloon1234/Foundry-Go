package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = expr.UUIDGreater(expr.Row(expr.Value(models.Orders.ID)).Value(), model.ID[models.Order]{})
