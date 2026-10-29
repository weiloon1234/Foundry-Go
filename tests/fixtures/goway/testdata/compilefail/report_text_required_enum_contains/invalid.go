package report_text_required_enum_contains

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextRow(orm.EnumExpr(models.Users.Status)).Contains(models.StatusActive)
