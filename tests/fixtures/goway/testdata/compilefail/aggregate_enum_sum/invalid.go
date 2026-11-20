package aggregate_enum_sum

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Sum(models.Users.Level)
