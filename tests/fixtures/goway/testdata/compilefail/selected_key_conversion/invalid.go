package selected_key_conversion

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.ProjectionKey[models.Order](expr.Key(expr.AsSelected(expr.Row(expr.Value(models.Users.Age)))))
