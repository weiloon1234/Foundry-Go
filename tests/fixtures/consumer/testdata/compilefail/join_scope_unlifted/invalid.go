package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type leftTag struct{}
type rightTag struct{}
type outsideTag struct{}

var a = rows.As[leftTag](models.Users.Query(), "a")
var b = rows.As[rightTag](models.Users.Query(), "b")
var af, bf = rows.Scope(a), rows.Scope(b)
var j = rows.LeftJoin(a, b, rows.On(expr.At(af, models.Users.IntroducerID), expr.At(bf, models.Users.ID)))
var invalid = rows.Select(j, reports.ReferralRows, rows.MapSelected(reports.ReferralRows.Email, expr.Select(expr.At(af, models.Users.Email))))
