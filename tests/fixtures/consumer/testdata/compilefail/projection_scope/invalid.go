package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.Select(rows.From(models.Orders.Query()), reports.UserSummaries, rows.Map(reports.UserSummaries.ID, expr.Value(orm.KeyExpr(models.Users.ID))))
