package subqueries

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
	count := rows.SelectScalar(rows.From(models.Orders.Query()), expr.Count[models.Order]().Value())
	nullable := rows.SelectScalar(outer, expr.Select(expr.Value(models.Users.Nickname)))
	ids := rows.SelectScalar(outer, expr.Select(expr.Value(models.Users.ID)))
	return rows.Select(outer, schema, rows.MapSelected(schema.Count, rows.ScalarQuery(outer, count)), rows.MapSelected(schema.Nickname, rows.ScalarNullableQuery(outer, nullable))).Where(rows.ExistsQuery(outer, count), rows.InQuery(expr.Value(models.Users.ID), ids), expr.Not(rows.InNullableQuery(expr.Value(models.Users.Email), nullable)))
}
