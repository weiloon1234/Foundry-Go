package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.OnConflictKeys[models.User](expr.Key(expr.AsSelected(expr.Row(expr.Value(models.Users.Age)))))
