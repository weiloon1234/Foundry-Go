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
	choice := expr.WhenValue(expr.GtValue(count, 0), expr.AsSelected(count)).When(expr.GtValue(count, 10), expr.AsSelected(expr.ParamOf(count, 10))).Else(expr.AsSelected(expr.ParamOf(count, 0)))
	nullable := expr.WhenValue(expr.GtValue(count, 0), expr.AsSelected(count)).ElseNull()
	required := expr.CoalesceValue(expr.NullIfValue(choice, expr.AsSelected(expr.ParamOf(count, 0))), expr.AsSelected(expr.ParamOf(count, 0)))
	missing := expr.NullIfNullableValue(expr.CoalesceNullableValue(nullable), nullable)
	return rows.Select(source, schema, rows.MapSelected(schema.Required, required), rows.MapSelected(schema.Missing, missing))
}
