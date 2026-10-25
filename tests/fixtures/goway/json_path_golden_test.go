package goway_test

import (
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func jsonOldSource() query.Query[jsonpaths.Document] {
	return query.ForModel(query.Define("json_documents", "id", []query.Column{{Name: "id"}, {Name: "settings"}, {Name: "backup", Nullable: true}, {Name: "tags"}, {Name: "fixed"}, {Name: "state"}}, func(database.Row) (jsonpaths.Document, error) { panic("selected result must use projection codec") }))
}
func jsonGolden[O, N any](t *testing.T, old query.Expression[jsonpaths.Document, O], next expr.Selected[jsonpaths.Document, N], c codec.Codec[N], sample driver.Value, op []query.Predicate[jsonpaths.Document], np []expr.Predicate[jsonpaths.Document]) {
	t.Helper()
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, g := range []*goldenCapture{before, after} {
		g.scriptedColumns = []string{"value"}
		g.scriptedBatches = [][][]driver.Value{{{sample}}}
	}
	ctx := t.Context()
	odb, ndb := before.open(ctx), after.open(ctx)
	defer odb.Close(ctx)
	defer ndb.Close(ctx)
	oldRows := must(query.SelectValue(jsonOldSource().Where(op...), old).All(ctx, odb))
	f := rowset.Define(func(b rowset.Builder[windowGoldenRow[N]], r *windowGoldenRow[N]) windowGoldenFields[N] {
		return windowGoldenFields[N]{b.Row(), rowset.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got := must(rowset.Select(rowset.FromModel(jsonpaths.Documents.Query()), f, rowset.MapSelected(f.Value, next)).Where(np...).All(ctx, ndb))
	values := make([]N, len(got))
	for i, r := range got {
		values[i] = r.Value
	}
	fmt.Fprintln(&before.output, oracleSnapshot([]any{oldRows}))
	fmt.Fprintln(&after.output, oracleSnapshot([]any{values}))
	if before.statements != 1 || after.statements != 1 || !oracleEqual(t, "json_path_golden/1", before.output.String(), after.output.String()) {
		t.Fatalf("JSON parity: %s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func TestStage6d3JSONGolden(t *testing.T) {
	if err := jsonpaths.Check(); err != nil {
		t.Fatal(err)
	}
	o := query.NewJSONField[jsonpaths.Document, value.JSON[jsonpaths.Preferences]]("json_documents", "settings", codec.JSON[jsonpaths.Preferences]())
	n := expr.Value(jsonpaths.Documents.Settings)
	ob := query.NewNullableJSONField[jsonpaths.Document, value.JSON[jsonpaths.Preferences]]("json_documents", "backup", codec.JSON[jsonpaths.Preferences]())
	nb := expr.Value(jsonpaths.Documents.Backup)
	ot := query.NewJSONField[jsonpaths.Document, value.JSON[[]string]]("json_documents", "tags", codec.JSON[[]string]())
	nt := expr.Value(jsonpaths.Documents.Tags)
	t.Run("JSONType/object", func(t *testing.T) {
		jsonGolden(t, query.JSONType(o).Value(), expr.JSONType(n).Value(), codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate), "object", nil, nil)
	})
	t.Run("JSONTypeValue/object", func(t *testing.T) {
		jsonGolden(t, query.JSONTypeValue(query.Value(o)), expr.JSONTypeValue(expr.Select(n)), codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate), "object", nil, nil)
	})
	t.Run("JSONTypeNullable/object", func(t *testing.T) {
		jsonGolden(t, query.JSONTypeNullable(ob).Value(), expr.JSONTypeNullable(nb).Value(), codec.Nullable(codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate)), "object", nil, nil)
	})
	t.Run("JSONTypeNullable/nil", func(t *testing.T) {
		jsonGolden(t, query.JSONTypeNullable(ob).Value(), expr.JSONTypeNullable(nb).Value(), codec.Nullable(codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate)), nil, nil, nil)
	})
	t.Run("JSONTypeNullableValue/object", func(t *testing.T) {
		jsonGolden(t, query.JSONTypeNullableValue(query.Value(ob)), expr.JSONTypeNullableValue(expr.Select(nb)), codec.Nullable(codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate)), "object", nil, nil)
	})
	t.Run("JSONTypeNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, query.JSONTypeNullableValue(query.Value(ob)), expr.JSONTypeNullableValue(expr.Select(nb)), codec.Nullable(codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate)), nil, nil, nil)
	})
	t.Run("JSONLength/int64(2)", func(t *testing.T) {
		jsonGolden(t, ot.Length().Value(), expr.JSONLength(nt).Value(), codec.Nullable(codec.Signed[int64]()), int64(2), nil, nil)
	})
	t.Run("JSONLength/nil", func(t *testing.T) {
		jsonGolden(t, ot.Length().Value(), expr.JSONLength(nt).Value(), codec.Nullable(codec.Signed[int64]()), nil, nil, nil)
	})
	t.Run("JSONLengthValue/int64(2)", func(t *testing.T) {
		jsonGolden(t, ot.Length().Value(), expr.JSONLengthValue(expr.Select(nt)), codec.Nullable(codec.Signed[int64]()), int64(2), nil, nil)
	})
	t.Run("JSONLengthValue/nil", func(t *testing.T) {
		jsonGolden(t, ot.Length().Value(), expr.JSONLengthValue(expr.Select(nt)), codec.Nullable(codec.Signed[int64]()), nil, nil, nil)
	})
	t.Run("JSONLengthNullable/int64(2)", func(t *testing.T) {
		jsonGolden(t, ob.Length().Value(), expr.JSONLengthNullable(nb).Value(), codec.Nullable(codec.Signed[int64]()), int64(2), nil, nil)
	})
	t.Run("JSONLengthNullable/nil", func(t *testing.T) {
		jsonGolden(t, ob.Length().Value(), expr.JSONLengthNullable(nb).Value(), codec.Nullable(codec.Signed[int64]()), nil, nil, nil)
	})
	t.Run("JSONLengthNullableValue/int64(2)", func(t *testing.T) {
		jsonGolden(t, ob.Length().Value(), expr.JSONLengthNullableValue(expr.Select(nb)), codec.Nullable(codec.Signed[int64]()), int64(2), nil, nil)
	})
	t.Run("JSONLengthNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, ob.Length().Value(), expr.JSONLengthNullableValue(expr.Select(nb)), codec.Nullable(codec.Signed[int64]()), nil, nil, nil)
	})
	t.Run("JSONContains/true", func(t *testing.T) {
		jsonGolden(t, query.JSONContains(o, o).Value(), expr.JSONContains(n, n).Value(), codec.Bool[bool](), true, nil, nil)
	})
	t.Run("JSONContainsValue/true", func(t *testing.T) {
		jsonGolden(t, query.JSONContainsValue(query.Value(o), query.Value(o)), expr.JSONContainsValue(expr.Select(n), expr.Select(n)), codec.Bool[bool](), true, nil, nil)
	})
	t.Run("JSONContainsNullable/true", func(t *testing.T) {
		jsonGolden(t, query.JSONContainsNullable(ob, ob).Value(), expr.JSONContainsNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), true, nil, nil)
	})
	t.Run("JSONContainsNullable/nil", func(t *testing.T) {
		jsonGolden(t, query.JSONContainsNullable(ob, ob).Value(), expr.JSONContainsNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), nil, nil, nil)
	})
	t.Run("JSONContainsNullableValue/true", func(t *testing.T) {
		jsonGolden(t, query.JSONContainsNullableValue(query.Value(ob), query.Value(ob)), expr.JSONContainsNullableValue(expr.Select(nb), expr.Select(nb)), codec.Nullable(codec.Bool[bool]()), true, nil, nil)
	})
	t.Run("JSONContainsNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, query.JSONContainsNullableValue(query.Value(ob), query.Value(ob)), expr.JSONContainsNullableValue(expr.Select(nb), expr.Select(nb)), codec.Nullable(codec.Bool[bool]()), nil, nil, nil)
	})
	t.Run("JSONContainedBy/false", func(t *testing.T) {
		jsonGolden(t, query.JSONContainedBy(o, o).Value(), expr.JSONContainedBy(n, n).Value(), codec.Bool[bool](), false, nil, nil)
	})
	t.Run("JSONContainedByValue/false", func(t *testing.T) {
		jsonGolden(t, query.JSONContainedByValue(query.Value(o), query.Value(o)), expr.JSONContainedByValue(expr.Select(n), expr.Select(n)), codec.Bool[bool](), false, nil, nil)
	})
	t.Run("JSONContainedByNullable/false", func(t *testing.T) {
		jsonGolden(t, query.JSONContainedByNullable(ob, ob).Value(), expr.JSONContainedByNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), false, nil, nil)
	})
	t.Run("JSONContainedByNullable/nil", func(t *testing.T) {
		jsonGolden(t, query.JSONContainedByNullable(ob, ob).Value(), expr.JSONContainedByNullable(nb, nb).Value(), codec.Nullable(codec.Bool[bool]()), nil, nil, nil)
	})
	t.Run("JSONContainedByNullableValue/false", func(t *testing.T) {
		jsonGolden(t, query.JSONContainedByNullableValue(query.Value(ob), query.Value(ob)), expr.JSONContainedByNullableValue(expr.Select(nb), expr.Select(nb)), codec.Nullable(codec.Bool[bool]()), false, nil, nil)
	})
	t.Run("JSONContainedByNullableValue/nil", func(t *testing.T) {
		jsonGolden(t, query.JSONContainedByNullableValue(query.Value(ob), query.Value(ob)), expr.JSONContainedByNullableValue(expr.Select(nb), expr.Select(nb)), codec.Nullable(codec.Bool[bool]()), nil, nil, nil)
	})

	oroot := query.JSONRoot(o)
	nroot := expr.JSONRoot(n)
	otheme := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Preferences, string](oroot, "theme", false)
	ntheme := expr.JSONOptional(expr.JSONProperty(nroot, jsonpaths.Theme))
	op := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Preferences, jsonpaths.Profile](oroot, "profile", false)
	np := expr.JSONOptional(expr.JSONProperty(nroot, jsonpaths.UserProfile))
	oage := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Profile, int64](op, "age", true)
	nage := expr.JSONProperty(np, jsonpaths.Age)
	okeys := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Profile, map[int]string](op, "attributes", false)
	nkeys := expr.JSONProperty(np, jsonpaths.Attributes)
	oscores := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Profile, []int64](op, "scores", false)
	nscores := expr.JSONProperty(np, jsonpaths.Scores)
	olabels := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Preferences, map[string]string](oroot, "labels", false)
	nlabels := expr.JSONOptional(expr.JSONProperty(nroot, jsonpaths.Labels))
	for _, sample := range []driver.Value{nil, "dark"} {
		jsonGolden(t, query.JSONTextScalar(otheme, codec.String[string]()).Value(), expr.JSONTextScalar(ntheme, codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), sample, nil, nil)
	}
	jsonGolden(t, query.JSONOrderedScalar(oage, codec.Signed[int64]()).Value(), expr.JSONOrderedScalar(nage, codec.Signed[int64]()).Value(), codec.Nullable(codec.Signed[int64]()), int64(42), nil, nil)
	jsonGolden(t, query.JSONTextScalar(query.NewJSONMapEntry[jsonpaths.Document, map[int]string, string](okeys, 2), codec.String[string]()).Value(), expr.JSONTextScalar(expr.JSONMapKey(nkeys, 2), codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), "two", nil, nil)
	jsonGolden(t, query.JSONTextScalar(query.NewJSONMapEntry[jsonpaths.Document, map[string]string, string](olabels, "runtime'key"), codec.String[string]()).Value(), expr.JSONTextScalar(expr.JSONMapKey(nlabels, "runtime'key"), codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), "label", nil, nil)
	jsonGolden(t, query.JSONOrderedScalar(query.NewJSONArrayElement[jsonpaths.Document, []int64, int64](oscores, -1), codec.Signed[int64]()).Value(), expr.JSONOrderedScalar(expr.JSONIndex(nscores, -1), codec.Signed[int64]()).Value(), codec.Nullable(codec.Signed[int64]()), int64(3), nil, nil)
	jsonGolden(t, oscores.Length().Value(), nscores.Length().Value(), codec.Nullable(codec.Signed[int64]()), int64(3), nil, nil)
	opred := []query.Predicate[jsonpaths.Document]{otheme.Exists(), otheme.IsMissing(), otheme.IsJSONNull(), oscores.Contains([]int64{2})}
	npred := []expr.Predicate[jsonpaths.Document]{ntheme.Exists(), ntheme.IsMissing(), ntheme.IsJSONNull(), nscores.Contains([]int64{2})}
	jsonGolden(t, otheme.Kind().Value(), ntheme.Kind().Value(), codec.Nullable(codec.String[expr.JSONKind]().Validated(expr.JSONKind.Validate)), "null", opred, npred)
	for _, sample := range []driver.Value{nil, "null", "\"hello\""} {
		onick := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Profile, *string](op, "nickname", false)
		nnick := expr.JSONProperty(np, jsonpaths.Nickname)
		jsonGolden(t, onick.JSON().Value(), nnick.JSON().Value(), codec.Nullable(codec.JSON[*string]()), sample, nil, nil)
	}
	oq := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Profile, string](op, "quoted", true)
	nq := expr.JSONProperty(np, jsonpaths.Quoted)
	jsonGolden(t, query.JSONTextScalar(oq, codec.String[string]()).Value(), expr.JSONTextScalar(nq, codec.String[string]()).Value(), codec.Nullable(codec.String[string]()), "quoted", nil, nil)
	of := query.NewJSONField[jsonpaths.Document, value.JSON[[3]int64]]("json_documents", "fixed", codec.JSON[[3]int64]())
	oldFixed := query.NewJSONArrayElement[jsonpaths.Document, [3]int64, int64](query.JSONRoot(of), -2)
	newFixed := expr.JSONArrayElement(expr.JSONRoot(expr.Value(jsonpaths.Documents.Fixed)), jsonpaths.FixedElements, -2)
	jsonGolden(t, query.JSONOrderedScalar(oldFixed, codec.Signed[int64]()).Value(), expr.JSONOrderedScalar(newFixed, codec.Signed[int64]()).Value(), codec.Nullable(codec.Signed[int64]()), int64(2), nil, nil)
}
