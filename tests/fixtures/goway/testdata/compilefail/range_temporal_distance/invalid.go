package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"time"
)

var _ = expr.NullableTemporalRange(expr.WindowFor(rows.ModelScope(models.Users.Query())), expr.Row(expr.Value(models.Users.Birthday)).Value()).Preceding(time.Hour)
