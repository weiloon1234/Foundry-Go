package jsonqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/jsonqueries"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestJSONNaturalKeyRelationFilterParity(t *testing.T) {
	if err := orm.Check(jsonqueries.Documents, jsonqueries.Policies, jsonqueries.DocumentLinks.Policy); err != nil {
		t.Fatal(err)
	}
	key := document(t, jsonqueries.PolicyKey{Version: 9007199254740993, Region: "apac"})

	r := jsonqueries.DocumentLinks.Policy
	id := jsonqueries.Policies.Key()
	r = r.Where(expr.RelationFilter(r, expr.Eq(expr.JSONContains(expr.Value(orm.KeyExpr(id)), expr.Param(orm.KeyExpr(id), key)), true)))
	after, err := rows.CompileRead(rows.Read(rows.FromModel(jsonqueries.Documents.Query().Where(orm.Has(r)))))
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/jsonqueries/relation_filter_test/1", oracle.Snapshot([]any{after.SQL(), after.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
}
