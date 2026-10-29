package goway_test

import "github.com/weiloon1234/Foundry-Go/value"

import "github.com/weiloon1234/Foundry-Go/database"

import (
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models/jsonpaths"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"testing"
)

func jsonGolden[N any](t *testing.T, next expr.Selected[jsonpaths.Document, N], c codec.Codec[N], sample driver.Value, np []expr.Predicate[jsonpaths.Document]) {
	t.Helper()
	after := &goldenCapture{}
	for _, g := range []*goldenCapture{after} {
		g.scriptedColumns = []string{"value"}
		g.scriptedBatches = [][][]driver.Value{{{sample}}}
	}
	ctx := t.Context()
	ndb := after.open(ctx)

	defer ndb.Close(ctx)

	f := rowset.Define(func(b rowset.Builder[windowGoldenRow[N]], r *windowGoldenRow[N]) windowGoldenFields[N] {
		return windowGoldenFields[N]{b.Row(), rowset.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got := must(rowset.Read(rowset.Select(rowset.FromModel(jsonpaths.Documents.Query()), f, rowset.MapSelected(f.Value, next)).Where(np...)).All(ctx, ndb))
	values := make([]N, len(got))
	for i, r := range got {
		values[i] = r.Value
	}

	fmt.Fprintln(&after.output, oracleSnapshot([]any{values}))
	if after.statements != 1 || !oracleEqual(t, "json_path_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}
func TestStage6d3JSONGolden(t *testing.T) {
	if err := jsonpaths.Check(); err != nil {
		t.Fatal(err)
	}

	n := expr.Value(orm.JSONExpr(jsonpaths.Documents.Settings))

	nb := expr.Value(orm.NullableJSONExpr(jsonpaths.Documents.Backup))

	nt := expr.Value(orm.JSONExpr(jsonpaths.Documents.Tags))
	t.Run("JSONType/object", func(t *testing.T) {
		jsonGolden(t, expr.JSONType(n).Value(), codec.String[database.JSONKind]().Validated(database.JSONKind.Validate), "object", nil)
	})
	t.Run("JSONTypeValue/object", func(t *testing.T) {
		jsonGolden(t, expr.JSONTypeValue(expr.Row(n).Value()), codec.String[database.JSONKind]().Validated(database.JSONKind.Validate), "object", nil)
	})
	t.Run("JSONTypeNullable/object", func(t *testing.T) {
		jsonGolden(t, expr.JSONTypeNullable(nb).Value(), codec.Nullable(codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)), "object", nil)
	})
	t.Run("JSONTypeNullable/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONTypeNullable(nb).Value(), codec.Nullable(codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)), nil, nil)
	})
	t.Run("JSONTypeNullableValue/object", func(t *testing.T) {
		jsonGolden(t, expr.JSONTypeNullableValue(expr.Row(nb).Value()), codec.Nullable(codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)), "object", nil)
	})
	t.Run("JSONTypeNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONTypeNullableValue(expr.Row(nb).Value()), codec.Nullable(codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)), nil, nil)
	})
	t.Run("JSONLength/int64(2)", func(t *testing.T) {
		jsonGolden(t, expr.JSONLength(nt).Value(), codec.Nullable(codec.Signed[int64]()), int64(2), nil)
	})
	t.Run("JSONLength/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONLength(nt).Value(), codec.Nullable(codec.Signed[int64]()), nil, nil)
	})
	t.Run("JSONLengthValue/int64(2)", func(t *testing.T) {
		jsonGolden(t, expr.JSONLengthValue(expr.Row(nt).Value()), codec.Nullable(codec.Signed[int64]()), int64(2), nil)
	})
	t.Run("JSONLengthValue/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONLengthValue(expr.Row(nt).Value()), codec.Nullable(codec.Signed[int64]()), nil, nil)
	})
	t.Run("JSONLengthNullable/int64(2)", func(t *testing.T) {
		jsonGolden(t, expr.JSONLengthNullable(nb).Value(), codec.Nullable(codec.Signed[int64]()), int64(2), nil)
	})
	t.Run("JSONLengthNullable/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONLengthNullable(nb).Value(), codec.Nullable(codec.Signed[int64]()), nil, nil)
	})
	t.Run("JSONLengthNullableValue/int64(2)", func(t *testing.T) {
		jsonGolden(t, expr.JSONLengthNullableValue(expr.Row(nb).Value()), codec.Nullable(codec.Signed[int64]()), int64(2), nil)
	})
	t.Run("JSONLengthNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONLengthNullableValue(expr.Row(nb).Value()), codec.Nullable(codec.Signed[int64]()), nil, nil)
	})
	t.Run("JSONContains/true", func(t *testing.T) {
		jsonGolden(t, expr.JSONContains(n, n).Value(), codec.Bool[bool](), true, nil)
	})
	t.Run("JSONContainsValue/true", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainsValue(expr.Row(n).Value(), expr.Row(n).Value()), codec.Bool[bool](), true, nil)
	})
	t.Run("JSONContainsNullable/true", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainsNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), true, nil)
	})
	t.Run("JSONContainsNullable/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainsNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), nil, nil)
	})
	t.Run("JSONContainsNullableValue/true", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainsNullableValue(expr.Row(nb).Value(), expr.Row(nb).Value()), codec.Nullable(codec.Bool[bool]()), true, nil)
	})
	t.Run("JSONContainsNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainsNullableValue(expr.Row(nb).Value(), expr.Row(nb).Value()), codec.Nullable(codec.Bool[bool]()), nil, nil)
	})
	t.Run("JSONContainedBy/false", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainedBy(n, n).Value(), codec.Bool[bool](), false, nil)
	})
	t.Run("JSONContainedByValue/false", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainedByValue(expr.Row(n).Value(), expr.Row(n).Value()), codec.Bool[bool](), false, nil)
	})
	t.Run("JSONContainedByNullable/false", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainedByNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), false, nil)
	})
	t.Run("JSONContainedByNullable/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainedByNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), nil, nil)
	})
	t.Run("JSONContainedByNullableValue/false", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainedByNullableValue(expr.Row(nb).Value(), expr.Row(nb).Value()), codec.Nullable(codec.Bool[bool]()), false, nil)
	})
	t.Run("JSONContainedByNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, expr.JSONContainedByNullableValue(expr.Row(nb).Value(), expr.Row(nb).Value()), codec.Nullable(codec.Bool[bool]()), nil, nil)
	})

	nroot := expr.JSONRoot(n)

	ntheme := expr.JSONOptional(expr.JSONProperty(nroot, jsonpaths.Theme))

	np := expr.JSONOptional(expr.JSONProperty(nroot, jsonpaths.UserProfile))

	nage := expr.JSONProperty(np, jsonpaths.Age)

	nkeys := expr.JSONProperty(np, jsonpaths.Attributes)

	nscores := expr.JSONProperty(np, jsonpaths.Scores)

	nlabels := expr.JSONOptional(expr.JSONProperty(nroot, jsonpaths.Labels))
	for _, sample := range []driver.Value{nil, "dark"} {
		jsonGolden(t, expr.JSONTextScalar(ntheme, codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), sample, nil)
	}
	jsonGolden(t, expr.OrderNullableRow(expr.JSONScalar(nage, codec.Signed[int64]())).Value(), codec.Nullable(codec.Signed[int64]()), int64(42), nil)
	jsonGolden(t, expr.JSONTextScalar(expr.JSONMapKey(nkeys, 2), codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), "two", nil)
	jsonGolden(t, expr.JSONTextScalar(expr.JSONMapKey(nlabels, "runtime'key"), codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), "label", nil)
	jsonGolden(t, expr.OrderNullableRow(expr.JSONScalar(expr.JSONIndex(nscores, -1), codec.Signed[int64]())).Value(), codec.Nullable(codec.Signed[int64]()), int64(3), nil)
	jsonGolden(t, expr.JSONLengthNullable(nscores.JSON()).Value(), codec.Nullable(codec.Signed[int64]()), int64(3), nil)

	npred := []expr.Predicate[jsonpaths.Document]{ntheme.JSON().IsNotNull(), ntheme.JSON().IsNull(), expr.JSONTypeNullable(ntheme.JSON()).Eq(value.Of(database.JSONNull)), expr.JSONContainsNullable(nscores.JSON(), nscores.JSON().Param(value.Of(must(value.NewJSON([]int64{2}))))).Eq(value.Of(true))}
	jsonGolden(t, expr.JSONTypeNullable(ntheme.JSON()).Value(), codec.Nullable(codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)), "null", npred)
	for _, sample := range []driver.Value{nil, "null", "\"hello\""} {
		nnick := expr.JSONProperty(np, jsonpaths.Nickname)
		jsonGolden(t, nnick.JSON().Value(), codec.Nullable(codec.JSON[*string]()), sample, nil)
	}

	nq := expr.JSONProperty(np, jsonpaths.Quoted)
	jsonGolden(t, expr.JSONTextScalar(nq, codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), "quoted", nil)

	newFixed := expr.JSONArrayElement(expr.JSONRoot(expr.Value(orm.JSONExpr(jsonpaths.Documents.Fixed))), jsonpaths.FixedElements, -2)
	jsonGolden(t, expr.OrderNullableRow(expr.JSONScalar(newFixed, codec.Signed[int64]())).Value(), codec.Nullable(codec.Signed[int64]()), int64(2), nil)
}
