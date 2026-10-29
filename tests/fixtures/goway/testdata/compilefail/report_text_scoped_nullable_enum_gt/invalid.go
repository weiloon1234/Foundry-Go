package report_text_scoped_nullable_enum_gt

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var field orm.NullableEnum[models.User, models.Status]

var _ = expr.TextNullableRowAt(rows.ModelScope(models.Users.Query()), orm.NullableEnumExpr(field)).Gt(models.StatusActive)
