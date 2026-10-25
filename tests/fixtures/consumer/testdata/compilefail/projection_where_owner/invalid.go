package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.Select(rows.From(models.Users.Query()), reports.UserSummaries).Where(expr.ModelPredicate(models.Orders.TotalCents.Gt(1)))
