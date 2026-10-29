package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type input struct {
	ID    int64
	Value value.JSON[[]string]
}
type fields struct {
	orm.Table[input, int64]
	Value orm.JSON[input, []string]
}

var table = orm.Define("samples", func(b orm.Builder[input], r *input) fields {
	return fields{orm.TableOf(b, &r.ID), orm.JSONOf(b, &r.Value)}
})

type output struct{ Value value.JSON[map[string]string] }
type resultFields struct {
	rows.Row[output]
	Value rows.Field[output, value.JSON[map[string]string]]
}

var result = rows.Define(func(b rows.Builder[output], r *output) resultFields {
	return resultFields{b.Row(), rows.JSONOf(b, &r.Value)}
})
var invalid = rows.Map(result.Value, expr.Value(orm.JSONExpr(table.Value)))
