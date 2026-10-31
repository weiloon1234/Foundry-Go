package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.TemporalRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age)))))
