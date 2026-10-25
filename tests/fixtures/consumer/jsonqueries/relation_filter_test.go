package jsonqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/jsonqueries"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestJSONNaturalKeyRelationFilterParity(t *testing.T) {
	if err := orm.Check(jsonqueries.Documents, jsonqueries.Policies, jsonqueries.DocumentLinks.Policy); err != nil {
		t.Fatal(err)
	}
	key := document(t, jsonqueries.PolicyKey{Version: 9007199254740993, Region: "apac"})
	before, err := jsonqueries.QueryJsonDocuments().WhereHas(jsonqueries.DocumentRelations().Policy.Where(query.JSONContains(jsonqueries.PolicyFields().ID, query.Param(jsonqueries.PolicyFields().ID, key)).Eq(true))).Compile()
	if err != nil {
		t.Fatal(err)
	}
	r := jsonqueries.DocumentLinks.Policy
	id := jsonqueries.Policies.Key()
	r = r.Where(expr.RelationFilter(r, expr.JSONContains(expr.Value(id), expr.Param(id, key)).Eq(true)))
	after, err := rows.Compile(rows.FromModel(jsonqueries.Documents.Query().Where(orm.Has(r))))
	if err != nil {
		t.Fatal(err)
	}
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) || !oracleEqual(t, "consumer/jsonqueries/relation_filter_test/1", oracle.Snapshot([]any{before.SQL(), before.Arguments()}), oracle.Snapshot([]any{after.SQL(), after.Arguments()})) {
		t.Fatalf("JSON relation SQL differs:\n%s\n%s\n%v\n%v", before.SQL(), after.SQL(), before.Arguments(), after.Arguments())
	}
}
