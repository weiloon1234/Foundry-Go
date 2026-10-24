package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = expr.UUIDGreater(expr.Select(expr.Value(models.Orders.ID)), model.ID[models.Order]{})
