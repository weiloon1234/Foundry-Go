package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.Having(rows.Select(rows.From(models.Orders.Query()), reports.BuyerTotalRows), expr.GtValue(expr.Count[models.User](), 1))
