package case_owner_conversion

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Case[models.Order, string](expr.Case[models.User, string]{})
