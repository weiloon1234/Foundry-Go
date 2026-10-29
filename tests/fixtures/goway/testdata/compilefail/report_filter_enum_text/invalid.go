package report_filter_enum_text

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.CompareRow(expr.Value(orm.EnumExpr(models.Users.Status))).Contains(models.StatusActive)
