package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.LagOr(expr.Row(expr.Value(orm.TextExpr(models.Users.Email))).Value(), 1, 123, expr.WindowFor(rows.ModelScope(models.Users.Query())))
