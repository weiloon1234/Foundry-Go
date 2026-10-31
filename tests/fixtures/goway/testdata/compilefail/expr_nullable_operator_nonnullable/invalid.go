package invalid

import (
	"foundry.test/goway/models"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var name = expr.TextRow(orm.TextExpr(models.Users.Email))
var invalid = expr.NullableEq[models.User, string](name, "Ada")
