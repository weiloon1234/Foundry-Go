package jsonqueries_test

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"
)

func TestDeclaredJSONPathSQLParity(t *testing.T) {
	f := jsonqueries.Documents
	if err := orm.Check(f, jsonqueries.Policies); err != nil {
		t.Fatal(err)
	}
	selected := rows.ReadScalar(jsonqueries.ProfileAgeQuery())
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
