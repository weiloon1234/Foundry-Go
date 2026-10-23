package aggregate_forged_ordered_min

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type forgedOrdered struct {
	orm.Enum[models.User, models.Level]
}

func (forgedOrdered) Gt(models.Level) orm.Predicate[models.User] {
	return orm.Predicate[models.User]{}
}

var _ = expr.Min[models.User, models.Level, models.Level](forgedOrdered{models.Users.Level})
