package expr_selected

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type side struct{}
type Result struct {
	Count  int64
	Exists bool
}
type fields struct {
	rows.Row[Result]
	Count  rows.Field[Result, int64]
	Exists rows.Field[Result, bool]
}

var schema = rows.Define(func(b rows.Builder[Result], r *Result) fields {
	return fields{b.Row(), rows.NumberOf(b, &r.Count), rows.FieldOf(b, &r.Exists, orm.Codec(codec.Bool[bool]()))}
})

// Query exercises selected arithmetic and both aggregate result types.
func Query() rows.Query[rows.Alias[side, models.Order], Result] {
	source := rows.As[side](models.Orders.Query(), "o")
	count := expr.Count[rows.Alias[side, models.Order]]()
	return rows.Select(source, schema, rows.MapSelected(schema.Count, expr.AddValue(expr.AsSelected(count), expr.AsSelected(expr.Row(expr.ParamOf(count, 1))))), rows.MapSelected(schema.Exists, expr.AsSelected(expr.Exists[rows.Alias[side, models.Order]]())))
}
