package jsonqueries_test

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"
)

func TestDeclaredJSONPathSQLParity(t *testing.T) {
	f := jsonqueries.Documents
	if err := orm.Check(f, jsonqueries.Policies); err != nil {
		t.Fatal(err)
	}
	profile := expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(expr.Value(f.Settings)), jsonqueries.ProfileField))
	age := expr.JSONOrderedScalar(expr.JSONProperty(profile, jsonqueries.AgeField), codec.Signed[int64]())
	selected := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(f.Query(), age.Eq(42))), age.Value()))
	if err := rows.PreflightRead(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	got, err := rows.CompileRead(selected)
	if err != nil {
		t.Fatal(err)
	}
	old := jsonqueries.DocumentFields().Settings.Properties().Profile.Properties().Age.Scalar()
	want, err := query.SelectValue(jsonqueries.QueryJsonDocuments().Where(old.Eq(42)), old.Value()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/jsonqueries/path_sql_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("JSON path parity\n%s\n%s\n%v\n%v", got.SQL(), want.SQL(), got.Arguments(), want.Arguments())
	}
}
