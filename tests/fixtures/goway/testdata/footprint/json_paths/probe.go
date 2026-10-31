package json_paths

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type result struct {
	Theme value.Nullable[string]
	Age   value.Nullable[int64]
	Key   value.Nullable[string]
}
type fields struct {
	rows.Row[result]
	Theme rows.Field[result, value.Nullable[string]]
	Age   rows.Field[result, value.Nullable[int64]]
	Key   rows.Field[result, value.Nullable[string]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NullableTextOf(b, &r.Theme), rows.NullableNumberOf(b, &r.Age), rows.NullableTextOf(b, &r.Key)}
})

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	if err := jsonpaths.Check(); err != nil {
		return nil, err
	}
	d := jsonpaths.Documents
	root := expr.JSONRoot(expr.Value(orm.JSONExpr(d.Settings)))
	theme := expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(root, jsonpaths.Theme)), codec.String[string]())
	profile := expr.JSONOptional(expr.JSONProperty(root, jsonpaths.UserProfile))
	age := expr.OrderNullableRow(expr.JSONScalar(expr.JSONProperty(profile, jsonpaths.Age), codec.Signed[int64]()))
	key := expr.JSONTextScalar(expr.JSONMapKey(expr.JSONProperty(profile, jsonpaths.Attributes), 2), codec.String[string]())
	return rows.Read(rows.Select(rows.FromModel(d.Query()), schema, rows.MapSelected(schema.Theme, expr.AsSelected(theme)), rows.MapSelected(schema.Age, expr.AsSelected(age)), rows.MapSelected(schema.Key, expr.AsSelected(key))).Where(expr.NullableTextLike(theme, "d%"), expr.NullableGt(age, 18))).All(ctx, db)
}
