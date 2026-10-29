package report_text_required_enum_like

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextRow(orm.EnumExpr(models.Users.Status)).Like(models.StatusActive)
