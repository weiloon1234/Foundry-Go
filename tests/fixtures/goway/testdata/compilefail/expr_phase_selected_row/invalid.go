package invalid

import (
	"foundry.test/goway/models"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var selected = expr.AsSelected(expr.TextRow(orm.TextExpr(models.Users.Email)))
var invalid = expr.Eq[models.User, string](selected, "Ada")
