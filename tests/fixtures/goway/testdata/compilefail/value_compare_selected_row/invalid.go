package value_compare_selected_row

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = expr.Equal(expr.Count[models.User]().Value(), expr.Count[models.User]().Value())
