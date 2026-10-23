package correlation_nullable_fields

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type aTag struct{}
type bTag struct{}

var a = rows.As[aTag](models.Users.Query(), "a")
var b = rows.As[bTag](models.Users.Query(), "b")
var joined = rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), models.Users.ID), expr.At(rows.Scope(b), models.Users.ID)))
var link = rows.Correlate(joined, rows.From(models.Orders.Query()))
var nullable = rows.NullableRightScope(joined, rows.Scope(b))

var _ = expr.At(rows.OuterNullableScope(link, nullable), models.Users.ID)
