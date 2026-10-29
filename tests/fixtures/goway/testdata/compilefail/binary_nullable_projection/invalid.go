package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type input struct {
	ID    int64
	Value value.Nullable[[]byte]
}
type fields struct {
	orm.Table[input, int64]
	Value orm.NullableColumn[input, []byte]
}

var table = orm.Define("samples", func(b orm.Builder[input], r *input) fields {
	return fields{orm.TableOf(b, &r.ID), orm.NullableBytesOf(b, &r.Value)}
})

type output struct{ Value []byte }
type resultFields struct {
	rows.Row[output]
	Value rows.Field[output, []byte]
}

var result = rows.Define(func(b rows.Builder[output], r *output) resultFields {
	return resultFields{b.Row(), rows.BytesOf(b, &r.Value)}
})
var invalid = rows.Map(result.Value, expr.Value(orm.NullableColumnExpr(table.Value)))
