package report_text_scoped_required_enum_gt

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.Gt(expr.TextRowAt(rows.ModelScope(models.Users.Query()), orm.EnumExpr(models.Users.Status)), models.StatusActive)
