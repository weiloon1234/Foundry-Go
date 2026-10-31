package report_text_nullable_enum_forged

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type forged struct {
	orm.NullableEnum[models.User, models.Status]
}

func (forged) Contains(models.Status) orm.Predicate[models.User] { return orm.Predicate[models.User]{} }
func (forged) orderedColumn()                                    {}

var _ = expr.NullableGt(expr.TextNullableRow[models.User, models.Status](forged{}), models.StatusActive)
