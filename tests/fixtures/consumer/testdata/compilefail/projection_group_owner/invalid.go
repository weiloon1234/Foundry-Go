package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.GroupBy(rows.Select(rows.From(models.Users.Query()), reports.UserSummaries), expr.Group(expr.Value(models.Orders.BuyerID)))
