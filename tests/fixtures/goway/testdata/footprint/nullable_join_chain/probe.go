package nullable_join_chain

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type left struct{}
type right struct{}
type third struct{}
type result struct{ Email value.Nullable[string] }
type fields struct {
	rows.Row[result]
	Email rows.Field[result, value.Nullable[string]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NullableTextOf(b, &r.Email)}
})

func F() rows.Query[rows.Cross[rows.Left[rows.Alias[left, models.User], rows.Alias[right, models.User]], rows.Alias[third, models.User]], result] {
	a, b, c := rows.As[left](models.Users.Query(), "a"), rows.As[right](models.Users.Query(), "b"), rows.As[third](models.Users.Query(), "c")
	j := rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), models.Users.ID), expr.At(rows.Scope(b), models.Users.ID)))
	chain := rows.CrossJoin(j, c)
	scope := rows.LeftNullableScope(chain, rows.NullableRightScope(j, rows.Scope(b)))
	return rows.Select(chain, schema, rows.Map(schema.Email, expr.AtNullable(scope, models.Users.Email)))
}
