package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type leftTag struct{}
type rightTag struct{}

var a = rows.As[leftTag](models.Users.Query(), "a")
var b = rows.As[rightTag](models.Users.Query(), "b")
var selected = rows.Select(a, reports.ReferralRows,
	rows.Map(reports.ReferralRows.ID, expr.At(rows.Scope(a), models.Users.ID)))
var invalid = selected.Where(expr.At(rows.Scope(b), models.Users.Email).Eq("x"))
