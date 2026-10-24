package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = expr.NullableNumericRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.Nullable(expr.Select(expr.Value(models.Users.Age)))).Preceding(value.Of(1))
