package subqueries

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type result struct {
	Count    value.Nullable[int64]
	Nickname value.Nullable[string]
}
type fields struct {
	rows.Row[result]
	Count    rows.Field[result, value.Nullable[int64]]
	Nickname rows.Field[result, value.Nullable[string]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NullableNumberOf(b, &r.Count), rows.NullableTextOf(b, &r.Nickname)}
})

func F() rows.Query[models.User, result] {
	outer := rows.From(models.Users.Query())
	count := rows.SelectScalar(rows.From(models.Orders.Query()), expr.AsSelected(expr.Count[models.Order]()))
	nullable := rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname)))))
	ids := rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(models.Users.ID)))))
	return rows.Select(outer, schema, rows.MapSelected(schema.Count, expr.AsSelected(rows.ScalarRowQuery(outer, count))), rows.MapSelected(schema.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(outer, nullable)))).Where(rows.ExistsQuery(outer, count), rows.InQuery(expr.Value(orm.KeyExpr(models.Users.ID)), ids), expr.Not(rows.InNullableQuery(expr.Value(orm.TextExpr(models.Users.Email)), nullable)))
}
