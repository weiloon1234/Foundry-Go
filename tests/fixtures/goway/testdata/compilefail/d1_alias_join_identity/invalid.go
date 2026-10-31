package invalid

import (
	"foundry.test/goway/models"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type leftTag struct{}
type rightTag struct{}
type otherTag struct{}

var a = rows.As[leftTag](models.Users.Query(), "a")
var b = rows.As[rightTag](models.Users.Query(), "b")
var af, bf = rows.Scope(a), rows.Scope(b)

var joined = rows.InnerJoin(a, b, rows.On(expr.At(af, orm.KeyExpr(models.Users.ID)), expr.At(bf, orm.KeyExpr(models.Users.ID))))
var name = expr.TextRowAt(rows.LeftScope(joined, af), orm.TextExpr(models.Users.Email))
var invalid expr.TextRowExpression[rows.Inner[rows.Alias[leftTag, models.User], rows.Alias[otherTag, models.User]], string] = name
