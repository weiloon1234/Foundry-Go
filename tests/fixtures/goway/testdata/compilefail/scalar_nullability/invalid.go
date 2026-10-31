package scalar_nullability

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

type aTag struct{}
type bTag struct{}
type cTag struct{}

var a = rows.As[aTag](models.Users.Query(), "a")
var b = rows.As[bTag](models.Users.Query(), "b")
var c = rows.As[cTag](models.Users.Query(), "c")
var ageA = expr.At(rows.Scope(a), orm.NumberExpr(models.Users.Age))
var ageB = expr.At(rows.Scope(b), orm.NumberExpr(models.Users.Age))
var outer = rows.From(models.Users.Query())
var inner = rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(models.Users.ID)))))

type result struct{ ID model.ID[models.User] }
type fields struct {
	rows.Row[result]
	ID rows.Field[result, model.ID[models.User]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.FieldOf(b, &r.ID, orm.Codec(codec.ID[models.User]()))}
})
var _ = rows.Select(outer, schema, rows.MapSelected(schema.ID, expr.AsSelected(rows.ScalarRowQuery(outer, inner))))
