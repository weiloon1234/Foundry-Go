package nullable_eq_null

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func F() {
	u := rows.As[struct{}](models.Users.Query(), "u")
	expr.At(rows.Scope(u), orm.NullableColumnExpr(models.Users.IntroducerID)).Eq(value.Null[model.ID[models.User]]())
}
