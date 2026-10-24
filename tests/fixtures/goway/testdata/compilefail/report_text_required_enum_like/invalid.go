package report_text_required_enum_like

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextRow(models.Users.Status).Like(models.StatusActive)
