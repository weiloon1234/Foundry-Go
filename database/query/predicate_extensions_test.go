package query

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

func TestPredicateExtensionsCompile(t *testing.T) {
	name := NewTextField[user, string]("public.users", "name", codec.String[string]())
	age := NewOrderedField[user, int]("public.users", "age", codec.Signed[int]())
	ratio := NewScalarField[user, float64]("public.users", "age", codec.Float[float64]())
	for _, test := range []struct {
		predicate Predicate[user]
		sql       string
		arguments []any
	}{
		{True[user](), "WHERE TRUE ", nil},
		{And[user](), "WHERE TRUE ", nil},
		{False[user](), "WHERE FALSE ", nil},
		{Or[user](), "WHERE FALSE ", nil},
		{And(True[user](), age.Gt(1)), `WHERE (TRUE AND ("public"."users"."age" > $1))`, []any{int64(1)}},
		{age.NotIn(1, 2), `WHERE ("public"."users"."age" <> ALL($1))`, []any{"{1,2}"}},
		{age.NotIn(), "WHERE TRUE ", nil},
		{ratio.NotIn(0.5), `WHERE ("public"."users"."age" NOT IN ($1))`, []any{0.5}},
		{age.Between(1, 9), `WHERE (("public"."users"."age" >= $1) AND ("public"."users"."age" <= $2))`, []any{int64(1), int64(9)}},
		{name.StartsWith("a_%!"), `WHERE ("public"."users"."name" LIKE $1 ESCAPE '!')`, []any{"a!_!%!!%"}},
		{name.EndsWith("z"), `WHERE ("public"."users"."name" LIKE $1 ESCAPE '!')`, []any{"%z"}},
		{name.IStartsWith("Ab"), `WHERE ("public"."users"."name" ILIKE $1 ESCAPE '!')`, []any{"Ab%"}},
		{name.IEndsWith("Ab"), `WHERE ("public"."users"."name" ILIKE $1 ESCAPE '!')`, []any{"%Ab"}},
		{name.ILike("a%"), `WHERE ("public"."users"."name" ILIKE $1)`, []any{"a%"}},
	} {
		statement, err := modelQuery().Where(test.predicate).Limit(1).Compile()
		if err != nil {
			t.Fatal(err)
		}
		sql := statement.SQL()
		if !strings.Contains(sql+" ", test.sql) {
			t.Fatalf("missing %q in %s", test.sql, sql)
		}
		arguments := statement.Arguments()
		if !reflect.DeepEqual(arguments[:len(arguments)-1], append([]any{}, test.arguments...)) {
			t.Fatalf("%s bound %v", sql, arguments)
		}
	}
	statement, err := modelQuery().OrderBy(age.Asc().NullsFirst(), name.Desc().NullsLast()).Compile()
	if err != nil || !strings.HasSuffix(statement.SQL(), `ORDER BY "public"."users"."age" ASC NULLS FIRST, "public"."users"."name" DESC NULLS LAST`) {
		t.Fatal("explicit NULL placement lost", statement.SQL(), err)
	}
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	if _, err := cursorQuery().OrderBy(rank.Asc().NullsFirst()).cursorPlan(CursorRequest[cursorRecord]{Size: 1}); err == nil || !strings.Contains(err.Error(), "NULLS") {
		t.Fatal("cursor pagination accepted explicit NULL placement", err)
	}
}
