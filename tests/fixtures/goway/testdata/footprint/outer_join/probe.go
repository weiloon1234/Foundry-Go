package outer_join

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type left struct{}
type right struct{}
type result struct {
	Email    value.Nullable[string]
	Nickname value.Nullable[string]
}
type fields struct {
	rows.Row[result]
	Email, Nickname rows.Field[result, value.Nullable[string]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NullableTextOf(b, &r.Email), rows.NullableTextOf(b, &r.Nickname)}
})

func F() rows.Query[rows.Full[rows.Alias[left, models.User], rows.Alias[right, models.User]], result] {
	a, b := rows.As[left](models.Users.Query(), "a"), rows.As[right](models.Users.Query(), "b")
	j := rows.FullJoin(a, b, rows.On(expr.At(rows.Scope(a), models.Users.ID), expr.At(rows.Scope(b), models.Users.ID)))
	l, r := rows.NullableLeftScope(j, rows.Scope(a)), rows.NullableRightScope(j, rows.Scope(b))
	return rows.Select(j, schema, rows.Map(schema.Email, expr.AtNullable(l, models.Users.Email)), rows.Map(schema.Nickname, expr.AtNullableValue(r, models.Users.Nickname)))
}
