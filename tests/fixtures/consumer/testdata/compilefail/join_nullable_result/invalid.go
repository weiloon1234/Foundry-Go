package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
var j = rows.LeftJoin(a, b, rows.On(expr.At(af, orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(bf, orm.KeyExpr(models.Users.ID))))
var right = rows.NullableRightScope(j, rows.Scope(b))
var invalid = rows.Select(j, reports.ReferralRows, rows.MapSelected(reports.ReferralRows.Email, expr.Row(expr.AtNullable(right, orm.TextExpr(models.Users.Email))).Value()))
