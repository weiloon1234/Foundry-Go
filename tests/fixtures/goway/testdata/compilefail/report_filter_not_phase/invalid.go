package report_filter_not_phase

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Not(expr.Count[models.User]().Gt(0))
