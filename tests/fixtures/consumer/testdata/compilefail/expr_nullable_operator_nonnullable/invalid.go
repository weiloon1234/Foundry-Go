package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var name = expr.TextRow(models.Users.Email)
var invalid = expr.NullableEq[models.User, string](name, "Ada")
