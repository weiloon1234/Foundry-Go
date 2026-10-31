package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.NullableNumericRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.Nullable(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age)))))).Preceding(value.Of(1))
