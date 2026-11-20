package lateral_outer

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type parentTag struct{}
type childTag struct{}
type namedTag struct{}
type otherTag struct{}

var a = rows.As[parentTag](models.Users.Query(), "a")
var b = rows.As[childTag](models.Orders.Query(), "b")
var link = rows.Correlate(a, b)
var inner = rows.InnerScope(link, rows.Scope(b))
var innerID = expr.At(inner, models.Orders.ID)
var record = rows.SelectCorrelatedRecord(link, inner)
var lateral = rows.AsLateral[namedTag](record, "lateral_record")

type result struct{ Count int64 }
type fields struct {
	rows.Row[result]
	Count rows.Field[result, int64]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields { return fields{b.Row(), rows.NumberOf(b, &r.Count)} })

var _ = rows.CrossJoinLateral(rows.As[otherTag](models.Users.Query(), "other"), lateral)
