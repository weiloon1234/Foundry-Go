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
	choice := expr.When(nickname.IsNull(), name).When(name.Eq(""), expr.Row(name).Value().Param("fallback")).Else(name)
	nullable := expr.When(name.Ne(""), choice).ElseNull()
	missing := expr.NullIfNullable(expr.CoalesceNullable(nullable, nickname), expr.NullableRow(name).Param(value.Null[string]()))
	return rows.OrderBy(rows.Select(source, schema, rows.MapSelected(schema.Required, expr.Coalesce(expr.NullIf(choice, name), choice).Value()), rows.MapSelected(schema.Missing, missing.Value())).Where(choice.Ne("")), choice.Asc())
}
