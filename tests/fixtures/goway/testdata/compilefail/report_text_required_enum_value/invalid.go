package report_text_required_enum_value

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextRow(expr.Value(models.Users.Status)).Contains(models.StatusActive)
