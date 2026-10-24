package report_text_scoped_required_enum_gt

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = expr.TextRowAt(rows.ModelScope(models.Users.Query()), models.Users.Status).Gt(models.StatusActive)
