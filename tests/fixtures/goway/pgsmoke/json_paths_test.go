package pgsmoke_test

import (
	"testing"

	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func readJSON[V any](t *testing.T, ex database.Executor, v expr.Selected[jsonpaths.Document, V], c codec.Codec[V]) V {
	t.Helper()
	s := rows.Define(func(b rows.Builder[windowResult[V]], r *windowResult[V]) windowFields[V] {
		return windowFields[V]{b.Row(), rows.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got, err := rows.Read(rows.Select(rows.FromModel(jsonpaths.Documents.Query()), s, rows.MapSelected(s.Value, v))).All(t.Context(), ex)
	must(t, err)
	if len(got) != 1 {
		t.Fatal(got)
	}
	return got[0].Value
}
func TestStage6d3JSONPaths(t *testing.T) {
	must(t, jsonpaths.Check())
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE json_documents(id bigint PRIMARY KEY,settings jsonb NOT NULL,backup jsonb,tags jsonb NOT NULL,fixed jsonb NOT NULL,state jsonb NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO json_documents VALUES(1,'{"profile":{"name":"Ada","age":"42","nickname":null,"scores":[1,2,3],"attributes":{"2":"two"},"next":null,"quoted":"\"hello\"","odd%path":"odd"}}',NULL,'["a","b"]','[1,2,3]','null')`)
	must(t, err)
	d := jsonpaths.Documents
	root := expr.JSONRoot(expr.Value(d.Settings))
	profile := expr.JSONOptional(expr.JSONProperty(root, jsonpaths.UserProfile))
	theme := expr.JSONOptional(expr.JSONProperty(root, jsonpaths.Theme))
	nick := expr.JSONProperty(profile, jsonpaths.Nickname)
	age := expr.JSONProperty(profile, jsonpaths.Age)
	scores := expr.JSONProperty(profile, jsonpaths.Scores)
	fragment, err := value.NewJSON([]int64{2})
	must(t, err)
	if k := readJSON(t, db, expr.JSONType(expr.Value(d.Settings)).Value(), codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)); k != database.JSONObject {
		t.Fatal(k)
	}
	if v := readJSON(t, db, expr.JSONTypeNullable(expr.Value(d.Backup)).Value(), codec.Nullable(codec.String[database.JSONKind]())); !v.IsNull() {
		t.Fatal(v)
	}
	if v := readJSON(t, db, expr.JSONTypeNullable(theme.JSON()).Value(), codec.Nullable(codec.String[database.JSONKind]())); !v.IsNull() {
		t.Fatal(v)
	}
	if v := readJSON(t, db, expr.JSONTypeNullable(nick.JSON()).Value(), codec.Nullable(codec.String[database.JSONKind]())); v != value.Of(database.JSONNull) {
		t.Fatal(v)
	}
	if v := readJSON(t, db, nick.JSON().Value(), codec.Nullable(codec.JSON[*string]())); v.IsNull() {
		t.Fatal("JSON null lost presence")
	} else {
		j, _ := v.Get()
		if !j.IsJSONNull() {
			t.Fatal(j)
		}
	}
	if v := readJSON(t, db, expr.JSONTextScalar(expr.JSONPointer(nick), codec.String[string]()).Value(), codec.Nullable(codec.String[string]())); !v.IsNull() {
		t.Fatal(v)
	}
	if v := readJSON(t, db, expr.OrderNullableRow(expr.JSONScalar(age, codec.Signed[int64]())).Value(), codec.Nullable(codec.Signed[int64]())); v != value.Of(int64(42)) {
		t.Fatal(v)
	}
	if v := readJSON(t, db, expr.OrderNullableRow(expr.JSONScalar(expr.JSONIndex(scores, -1), codec.Signed[int64]())).Value(), codec.Nullable(codec.Signed[int64]())); v != value.Of(int64(3)) {
		t.Fatal(v)
	}
	if v := readJSON(t, db, expr.JSONTextScalar(expr.JSONMapKey(expr.JSONProperty(profile, jsonpaths.Attributes), 2), codec.String[string]()).Value(), codec.Nullable(codec.String[string]())); v != value.Of("two") {
		t.Fatal(v)
	}
	if v := readJSON(t, db, expr.JSONTextScalar(expr.JSONProperty(profile, jsonpaths.Quoted), codec.String[string]()).Value(), codec.Nullable(codec.String[string]())); v != value.Of("hello") {
		t.Fatal(v)
	}
	for _, p := range []expr.Predicate[jsonpaths.Document]{theme.JSON().IsNull(), nick.JSON().IsNotNull(), expr.JSONTypeNullable(nick.JSON()).Eq(value.Of(database.JSONNull)), expr.JSONContainsNullable(scores.JSON(), scores.JSON().Param(value.Of(fragment))).Eq(value.Of(true))} {
		n, err := expr.Where(d.Query(), p).Count(ctx, db)
		must(t, err)
		if n != 1 {
			t.Fatal(n)
		}
	}
	// Safe length never calls JSONB_ARRAY_LENGTH on non-array shapes.
	for _, json := range []string{`null`, `{}`, `"text"`, `42`, `true`, `[]`, `[null,1]`} {
		_, err = db.Exec(ctx, `UPDATE json_documents SET tags=$1::jsonb`, json)
		must(t, err)
		got := readJSON(t, db, expr.JSONLength(expr.Value(d.Tags)).Value(), codec.Nullable(codec.Signed[int64]()))
		switch json {
		case `[]`:
			if got != value.Of(int64(0)) {
				t.Fatal(got)
			}
		case `[null,1]`:
			if got != value.Of(int64(2)) {
				t.Fatal(got)
			}
		default:
			if !got.IsNull() {
				t.Fatal(got)
			}
		}
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE json_documents SET settings='{"theme":"transaction"}'::jsonb`)
		if err != nil {
			return err
		}
		if v := readJSON(t, tx, expr.JSONTextScalar(theme, codec.String[string]()).Value(), codec.Nullable(codec.String[string]())); v != value.Of("transaction") {
			t.Fatal(v)
		}
		return nil
	}))
}
