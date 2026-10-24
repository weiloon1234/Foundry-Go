package report_text_required_enum_gt

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextRow(models.Users.Status).Gt(models.StatusActive)
