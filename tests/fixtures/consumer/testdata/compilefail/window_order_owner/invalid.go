package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.WindowFor(rows.ModelScope(models.Users.Query())).OrderBy(expr.Desc(expr.Value(orm.NumberExpr(models.Orders.TotalCents))))
