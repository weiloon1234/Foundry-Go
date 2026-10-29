package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var source = rows.MatchNullableSource(orm.TextExpr(models.Users.Email), expr.Value(orm.NullableTextExpr(models.Users.Nickname)))
var _ = orm.UpdateMapping[models.User, models.User](source)
