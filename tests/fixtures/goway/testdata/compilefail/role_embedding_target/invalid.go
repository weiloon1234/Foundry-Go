package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type wrapped struct{ orm.Text[models.User, string] }

var _ = rows.MapUpdate(wrapped{models.Users.Email}, expr.Value(models.Users.Email))
