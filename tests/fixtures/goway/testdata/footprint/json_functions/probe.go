package json_functions

import (
	"context"
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type result struct {
	Kind     database.JSONKind
	Length   value.Nullable[int64]
	Contains value.Nullable[bool]
}
type fields struct {
	rows.Row[result]
	Kind     rows.Field[result, database.JSONKind]
	Length   rows.Field[result, value.Nullable[int64]]
	Contains rows.Field[result, value.Nullable[bool]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.FieldOf(b, &r.Kind, orm.Codec(codec.String[database.JSONKind]().Validated(database.JSONKind.Validate))), rows.NullableNumberOf(b, &r.Length), rows.FieldOf(b, &r.Contains, orm.Codec(codec.Nullable(codec.Bool[bool]())))}
})

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	d := jsonpaths.Documents
	return rows.Read(rows.Select(rows.FromModel(d.Query()), schema, rows.MapSelected(schema.Kind, expr.JSONTypeValue(expr.Row(expr.Value(orm.JSONExpr(d.Settings))).Value())), rows.MapSelected(schema.Length, expr.JSONLengthValue(expr.Row(expr.Value(orm.JSONExpr(d.Tags))).Value())), rows.MapSelected(schema.Contains, expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(d.Backup)), expr.NullableRow(expr.Value(orm.JSONExpr(d.Settings)))).Value()))).All(ctx, db)
}
