package trigramqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/trigramqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestTrigramExpressionFromConsumer(t *testing.T) {
	f := trigramqueries.Declare("example")
	name := expr.Value(orm.TextExpr(f.Names.Normalized))
	score := expr.TrigramSimilarity(name, trigramqueries.Normalized("words"))
	q := expr.Where(f.Names.Query(), expr.TrigramMatch(name, trigramqueries.Normalized("words")), expr.Gte(score, 0.6))
	statement, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(score))))
	want := `SELECT public.similarity("example"."entity_names"."normalized", $1) AS "value" FROM "example"."entity_names" WHERE ("example"."entity_names"."normalized" OPERATOR(public.%) $2) AND (public.similarity("example"."entity_names"."normalized", $3) >= $4)`
	if err != nil || statement.SQL() != want || !reflect.DeepEqual(statement.Arguments(), []any{"words", "words", "words", 0.6}) {
		t.Fatal(statement.SQL(), statement.Arguments(), err)
	}
}
