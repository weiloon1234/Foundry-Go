package raw_fragment_conversion

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.RowExpression[models.User, int64](expr.Raw[models.User, int64]("1"))
