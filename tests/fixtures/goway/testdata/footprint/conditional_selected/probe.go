package conditional_selected

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type side struct{}
type Result struct {
	Required int64
	Missing  value.Nullable[int64]
}
type fields struct {
	rows.Row[Result]
	Required rows.Field[Result, int64]
	Missing  rows.Field[Result, value.Nullable[int64]]
}

var schema = rows.Define(func(b rows.Builder[Result], r *Result) fields {
	return fields{b.Row(), rows.NumberOf(b, &r.Required), rows.NullableNumberOf(b, &r.Missing)}
})

// Query exercises selected CASE, COALESCE and NULLIF over aggregate values.
func Query() rows.Query[rows.Alias[side, models.Order], Result] {
	source := rows.As[side](models.Orders.Query(), "o")
	count := expr.Count[rows.Alias[side, models.Order]]()
	choice := expr.WhenValue(count.Gt(0), count.Value()).When(count.Gt(10), count.Param(10).Value()).Else(count.Param(0).Value())
	nullable := expr.WhenValue(count.Gt(0), count.Value()).ElseNull()
	required := expr.CoalesceValue(expr.NullIfValue(choice, count.Param(0).Value()), count.Param(0).Value())
	missing := expr.NullIfNullableValue(expr.CoalesceNullableValue(nullable), nullable)
	return rows.Select(source, schema, rows.MapSelected(schema.Required, required), rows.MapSelected(schema.Missing, missing))
}
