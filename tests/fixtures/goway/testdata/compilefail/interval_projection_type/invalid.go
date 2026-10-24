package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type input struct {
	ID    int64
	Value temporal.Interval
}
type fields struct {
	orm.Table[input, int64]
	Value orm.Interval[input]
}

var table = orm.Define("samples", func(b orm.Builder[input], r *input) fields {
	return fields{orm.TableOf(b, &r.ID), orm.IntervalOf(b, &r.Value)}
})

type output struct{ Value int64 }
type resultFields struct {
	rows.Row[output]
	Value rows.Field[output, int64]
}

var result = rows.Define(func(b rows.Builder[output], r *output) resultFields {
	return resultFields{b.Row(), rows.NumberOf(b, &r.Value)}
})
var invalid = rows.MapSelected(result.Value, expr.SumInterval(table.Value).Value())
