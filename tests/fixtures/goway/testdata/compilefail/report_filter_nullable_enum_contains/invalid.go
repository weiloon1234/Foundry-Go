package report_filter_nullable_enum_contains

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var field orm.NullableEnum[models.User, models.Status]

var _ = expr.NullableTextContains[models.User, models.Status](expr.CompareNullableRow(expr.Value(orm.NullableEnumExpr(field))), models.StatusActive)
