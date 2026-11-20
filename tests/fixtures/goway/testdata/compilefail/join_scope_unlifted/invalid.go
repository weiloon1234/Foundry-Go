package join_scope_unlifted

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type aTag struct{}
type bTag struct{}
type cTag struct{}

var a = rows.As[aTag](models.Users.Query(), "a")
var b = rows.As[bTag](models.Users.Query(), "b")
var c = rows.As[cTag](models.Users.Query(), "c")
var on = rows.On(expr.At(rows.Scope(a), models.Users.IntroducerID), expr.At(rows.Scope(b), models.Users.ID))
var j = rows.LeftJoin(a, b, on)

type result struct{ Email string }
type fields struct {
	rows.Row[result]
	Email rows.Field[result, string]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields { return fields{b.Row(), rows.TextOf(b, &r.Email)} })
var _ = rows.Select(j, schema, rows.Map(schema.Email, expr.At(rows.Scope(a), models.Users.Email)))
