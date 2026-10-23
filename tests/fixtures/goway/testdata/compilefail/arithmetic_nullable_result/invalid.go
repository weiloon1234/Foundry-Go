package arithmetic_nullable_result

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var age = expr.Value(models.Users.Age)
var email = expr.Value(models.Users.Email)
var _ = func() {
	var _ expr.Selected[models.User, int] = expr.AddNullable(expr.NullableRow(age), expr.NullableRow(age)).Value()
}
