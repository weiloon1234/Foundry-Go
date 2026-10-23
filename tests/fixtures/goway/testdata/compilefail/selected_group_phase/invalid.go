package selected_group_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.GroupBy(rows.SelectValue(models.Users.Query(), models.Users.Age), expr.Count[models.User]().Value().Key())
