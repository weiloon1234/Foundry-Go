package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() {
	_ = expr.Where(models.Users.Query(), expr.Year(expr.Value(orm.TimeExpr(scalars.Records.Date))).Eq(2024))
}
