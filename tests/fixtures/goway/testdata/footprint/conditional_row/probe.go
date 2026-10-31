package conditional_row

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type side struct{}
type Result struct {
	Required string
	Missing  value.Nullable[string]
}
type fields struct {
	rows.Row[Result]
	Required rows.Field[Result, string]
	Missing  rows.Field[Result, value.Nullable[string]]
}

var schema = rows.Define(func(b rows.Builder[Result], r *Result) fields {
	return fields{b.Row(), rows.TextOf(b, &r.Required), rows.NullableTextOf(b, &r.Missing)}
})

// Query exercises row CASE, COALESCE, NULLIF and nullable result composition.
func Query() rows.Query[rows.Alias[side, models.User], Result] {
	source := rows.As[side](models.Users.Query(), "u")
	name := expr.At(rows.Scope(source), orm.TextExpr(models.Users.Email))
	nickname := expr.At(rows.Scope(source), orm.NullableTextExpr(models.Users.Nickname))
	choice := expr.When(expr.IsNull(nickname), name).When(expr.Eq(name, ""), expr.ParamOf(expr.AsSelected(expr.Row(name)), "fallback")).Else(name)
	nullable := expr.When(expr.Ne(name, ""), choice).ElseNull()
	missing := expr.NullIfNullable(expr.CoalesceNullable(nullable, nickname), expr.ParamOf(expr.NullableRow(name), value.Null[string]()))
	return rows.OrderBy(rows.Select(source, schema, rows.MapSelected(schema.Required, expr.AsSelected(expr.Coalesce(expr.NullIf(choice, name), choice))), rows.MapSelected(schema.Missing, expr.AsSelected(missing))).Where(expr.Ne(choice, "")), expr.Asc(choice))
}
