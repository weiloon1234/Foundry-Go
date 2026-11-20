package report_text_required_enum_like

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.TextLike(expr.TextRow(models.Users.Status), models.StatusActive)
