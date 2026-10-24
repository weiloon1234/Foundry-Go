package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var source = rows.MatchNullableSource(models.Users.Email, expr.Value(models.Users.Nickname))
var _ = orm.InsertMapping[models.User, models.User](source)
