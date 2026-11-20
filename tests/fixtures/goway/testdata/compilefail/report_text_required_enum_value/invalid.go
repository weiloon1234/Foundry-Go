package report_text_required_enum_value

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextContains(expr.TextRow(expr.Value(models.Users.Status)), models.StatusActive)
