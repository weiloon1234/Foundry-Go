package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var r = expr.NumericRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age)))))
var _ = r.Between(expr.NumericRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.AsSelected(expr.Count[models.User]())).Preceding(1), r.CurrentRow())
