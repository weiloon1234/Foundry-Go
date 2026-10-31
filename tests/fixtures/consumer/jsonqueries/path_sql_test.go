package jsonqueries_test

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"
)

func TestDeclaredJSONPathSQLParity(t *testing.T) {
	f := jsonqueries.Documents
	if err := orm.Check(f, jsonqueries.Policies); err != nil {
		t.Fatal(err)
	}
	profile := expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(expr.Value(orm.JSONExpr(f.Settings))), jsonqueries.ProfileField))
	age := expr.OrderNullableRow(expr.JSONScalar(expr.JSONProperty(profile, jsonqueries.AgeField), codec.Signed[int64]()))
	selected := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(f.Query(), expr.NullableEq(age, 42))), expr.AsSelected(age)))
	if err := rows.PreflightRead(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	got, err := rows.CompileRead(selected)
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/jsonqueries/path_sql_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
}
