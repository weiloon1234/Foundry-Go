package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type leftTag struct{}

var a = rows.As[leftTag](models.Users.Query(), "a")
var invalid = rows.Map(reports.ReferralRows.Email, expr.At(rows.Scope(a), orm.NumberExpr(models.Users.Age)))
