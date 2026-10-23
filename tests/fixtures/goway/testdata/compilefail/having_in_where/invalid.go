package having_in_where

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Where(models.Orders.Query(), expr.Count[models.Order]().Gt(1))
