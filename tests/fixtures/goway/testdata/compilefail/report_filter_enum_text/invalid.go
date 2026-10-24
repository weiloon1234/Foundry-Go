package report_filter_enum_text

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.CompareRow(expr.Value(models.Users.Status)).Contains(models.StatusActive)
