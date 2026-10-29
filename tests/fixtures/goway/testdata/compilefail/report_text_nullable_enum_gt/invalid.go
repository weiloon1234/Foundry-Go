package report_text_nullable_enum_gt

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var field orm.NullableEnum[models.User, models.Status]

var _ = expr.TextNullableRow(orm.NullableEnumExpr(field)).Gt(models.StatusActive)
