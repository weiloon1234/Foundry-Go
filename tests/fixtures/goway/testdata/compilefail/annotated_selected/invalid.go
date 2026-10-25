package invalid

import (
	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.WithValue(models.Users.Query(), expr.Count[models.User]().Value())
