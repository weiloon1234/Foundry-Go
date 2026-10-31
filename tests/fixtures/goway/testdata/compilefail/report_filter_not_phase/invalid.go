package report_filter_not_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Not(expr.GtValue(expr.Count[models.User](), 0))
