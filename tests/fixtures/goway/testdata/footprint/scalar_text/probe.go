package scalar_text

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type result struct {
	Name   string
	Nick   value.Nullable[string]
	Length int64
}
type fields struct {
	rows.Row[result]
	Name   rows.Field[result, string]
	Nick   rows.Field[result, value.Nullable[string]]
	Length rows.Field[result, int64]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.TextOf(b, &r.Name), rows.NullableTextOf(b, &r.Nick), rows.NumberOf(b, &r.Length)}
})

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	name := expr.Lower(expr.Trim(expr.Value(models.Users.Email)))
	nick := expr.SubstringNullable(expr.Value(models.Users.Nickname), 1, 3)
	q := rows.Select(rows.FromModel(models.Users.Query()), schema, rows.MapSelected(schema.Name, name.Value()), rows.MapSelected(schema.Nick, nick.Value()), rows.MapSelected(schema.Length, expr.Length(name).Value())).Where(name.IContains("@example"))
	return rows.Read(q).All(ctx, db)
}
